package goanalyzer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pingli/axiom-insight/compiler/semantic"
)

func TestAnalyzeLoadsMultiplePackagesAndResolvesService(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":                          "module example.com/orders\n\ngo 1.26.1\n",
		"si.yaml":                         "service:\n  name: order-service\n",
		"main.go":                         "package orders\n\nfunc CreateOrder() {}\n",
		"internal/inventory/inventory.go": "package inventory\n\nfunc Reserve() {}\n",
		".hidden/ignored.go":              "package ignored\n\nfunc Hidden() {}\n",
		"vendor/thirdparty/ignored.go":    "package thirdparty\n\nfunc Ignored() {}\n",
		"main_test.go":                    "package orders\n\nfunc TestCreateOrder() {}\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze project: %v", err)
	}
	if document.Service.Name != "order-service" {
		t.Fatalf("service name = %q, want order-service", document.Service.Name)
	}
	if document.Service.ModulePath != "example.com/orders" {
		t.Fatalf("module path = %q, want example.com/orders", document.Service.ModulePath)
	}
	if len(document.Packages) != 2 {
		t.Fatalf("package count = %d, want 2: %+v", len(document.Packages), document.Packages)
	}
	for _, pkg := range document.Packages {
		for _, file := range pkg.Files {
			if file == "main_test.go" || file == ".hidden/ignored.go" || file == "vendor/thirdparty/ignored.go" {
				t.Fatalf("excluded file loaded: %q", file)
			}
			if filepath.IsAbs(file) {
				t.Fatalf("package file is absolute: %q", file)
			}
		}
	}
}

func TestLoadPackagesRequestsSyntaxTypesAndImports(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod": "module example.com/typed\n\ngo 1.26.1\n",
		"run.go": "package typed\n\nimport \"fmt\"\n\nfunc Run() { fmt.Println(\"ok\") }\n",
	})

	loaded, err := loadPackages(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("package count = %d, want 1", len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Syntax) == 0 {
		t.Fatal("syntax was not loaded")
	}
	if pkg.Types == nil {
		t.Fatal("types were not loaded")
	}
	if pkg.TypesInfo == nil {
		t.Fatal("types info was not loaded")
	}
	if len(pkg.Imports) == 0 {
		t.Fatal("imports were not loaded")
	}
}

func TestAnalyzeIncludesTestFilesWhenRequested(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":      "module example.com/testable\n\ngo 1.26.1\n",
		"run.go":      "package testable\n\nfunc Run() {}\n",
		"run_test.go": "package testable\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) {}\n",
	})

	withoutTests, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze without tests: %v", err)
	}
	withTests, err := Analyze(context.Background(), root, Options{IncludeTests: true})
	if err != nil {
		t.Fatalf("analyze with tests: %v", err)
	}
	if hasPackageFile(withoutTests, "run_test.go") {
		t.Fatal("test file included by default")
	}
	if !hasPackageFile(withTests, "run_test.go") {
		t.Fatal("test file missing when IncludeTests is true")
	}
}

func TestAnalyzeReportsGoParseErrors(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":    "module example.com/broken\n\ngo 1.26.1\n",
		"broken.go": "package broken\n\nfunc Broken( {\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze malformed project: %v", err)
	}
	if !hasDiagnostic(document, "GO_PARSE_ERROR") && !hasDiagnostic(document, "PACKAGE_LOAD_ERROR") {
		t.Fatalf("missing parse diagnostic: %+v", document.Diagnostics)
	}
}

func TestAnalyzeLoadsProjectWithoutGoModule(t *testing.T) {
	root := writeProject(t, map[string]string{
		"run.go": "package standalone\n\nfunc Run() {}\n",
	})

	document, err := Analyze(context.Background(), root, Options{})
	if err != nil {
		t.Fatalf("analyze project without go.mod: %v", err)
	}
	if document.Service.Name != filepath.Base(root) {
		t.Fatalf("service name = %q, want directory name %q", document.Service.Name, filepath.Base(root))
	}
	if len(document.Packages) != 1 {
		t.Fatalf("package count = %d, want 1: %+v", len(document.Packages), document.Packages)
	}
}

func TestAnalyzeRejectsInvalidSourceRoot(t *testing.T) {
	_, err := Analyze(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{})
	if err == nil {
		t.Fatal("Analyze succeeded for missing source root")
	}
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture file: %v", err)
		}
	}
	return root
}

func hasPackageFile(document semantic.Document, wanted string) bool {
	for _, pkg := range document.Packages {
		for _, file := range pkg.Files {
			if file == wanted {
				return true
			}
		}
	}
	return false
}

func hasDiagnostic(document semantic.Document, wanted string) bool {
	for _, diagnostic := range document.Diagnostics {
		if diagnostic.Code == wanted {
			return true
		}
	}
	return false
}

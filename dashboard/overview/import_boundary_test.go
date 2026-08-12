package overview

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestImportBoundary is the negative compile boundary check: the overview
// package must never transitively import Analyzer, AST, plugin transport,
// generated-document YAML contracts, network or environment access.
func TestImportBoundary(t *testing.T) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(config, "github.com/zhuyanxi/axiom-insight/dashboard/overview")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	if len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("package failed to load: %v", err)
	}

	forbiddenExact := []string{
		"go/ast",
		"go/types",
		"go/packages",
		"go/parser",
		"go/scanner",
		"github.com/zhuyanxi/axiom-insight/plugins",
		"github.com/zhuyanxi/axiom-insight/generator",
	}
	forbiddenPrefixes := []string{
		"github.com/zhuyanxi/axiom-insight/compiler/",
		"github.com/zhuyanxi/axiom-insight/generator/schemacheck",
	}

	seen := make(map[string]bool)
	var visit func(pkg *packages.Package)
	visit = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		for _, forbiddenPath := range forbiddenExact {
			if pkg.PkgPath == forbiddenPath {
				t.Errorf("overview transitively imports forbidden package %q", pkg.PkgPath)
			}
		}
		for _, forbiddenPath := range forbiddenPrefixes {
			if strings.HasPrefix(pkg.PkgPath, forbiddenPath) {
				t.Errorf("overview transitively imports forbidden package %q", pkg.PkgPath)
			}
		}
		for _, imported := range pkg.Imports {
			visit(imported)
		}
	}
	visit(pkgs[0])
}

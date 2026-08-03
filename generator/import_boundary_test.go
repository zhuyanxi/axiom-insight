package generator

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestGeneratorPackageImportBoundary is the negative compile boundary check:
// the package that owns the YAML contract must never import Analyzer or AST
// packages, transitively.
func TestGeneratorPackageImportBoundary(t *testing.T) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps,
	}
	pkgs, err := packages.Load(config, "github.com/zhuyanxi/axiom-insight/generator")
	if err != nil {
		t.Fatalf("load package: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected exactly one package, got %d", len(pkgs))
	}
	if len(pkgs[0].Errors) > 0 {
		t.Fatalf("package failed to load: %v", pkgs[0].Errors)
	}

	forbidden := []string{
		"go/ast",
		"go/types",
		"go/packages",
		"go/parser",
		"go/scanner",
		"github.com/zhuyanxi/axiom-insight/compiler/",
		"github.com/zhuyanxi/axiom-insight/plugins",
	}

	seen := make(map[string]bool)
	var visit func(pkg *packages.Package)
	visit = func(pkg *packages.Package) {
		if pkg == nil || seen[pkg.PkgPath] {
			return
		}
		seen[pkg.PkgPath] = true
		for _, forbiddenPath := range forbidden {
			if pkg.PkgPath == forbiddenPath || strings.HasPrefix(pkg.PkgPath, forbiddenPath) {
				t.Errorf("generator transitively imports forbidden package %q", pkg.PkgPath)
			}
		}
		for _, imported := range pkg.Imports {
			visit(imported)
		}
	}
	visit(pkgs[0])
}

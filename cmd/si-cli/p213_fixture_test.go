package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestP213ExpectedReportFixture validates the committed CLI report fixture
// against the same closed schema used by report contract tests.
func TestP213ExpectedReportFixture(t *testing.T) {
	reportSchema, err := os.ReadFile(filepath.Join("..", "..", "schemas", "dashboard", "v1", "cli-dashboard-report.schema.json"))
	if err != nil {
		t.Fatalf("read report schema: %v", err)
	}
	report, err := os.ReadFile(filepath.Join("..", "..", "testdata", "dashboard", "v1", "cli", "expected-report.json"))
	if err != nil {
		t.Fatalf("read report fixture: %v", err)
	}
	if err := schemacheck.Validate(reportSchema, report); err != nil {
		t.Fatalf("report fixture violates schema: %v", err)
	}
}

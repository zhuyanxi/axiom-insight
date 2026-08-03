package generator

import (
	"os"
	"path/filepath"
	"testing"
)

// fixturePath resolves a fixture under testdata/generator/v1 relative to the
// generator package directory.
func fixturePath(relative string) string {
	return filepath.Join("..", "testdata", "generator", "v1", filepath.FromSlash(relative))
}

func readFixture(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(relative))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relative, err)
	}
	return data
}

func schemaPath(name string) string {
	return filepath.Join("..", "schemas", "generator", "v1", name)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// hasViolationAt reports whether any violation's field matches wantField as a
// substring.
func hasViolationAt(violations []*ValidationError, wantField string) bool {
	for _, violation := range violations {
		if violation.Field == wantField {
			return true
		}
	}
	return false
}

func anyViolation(violations []*ValidationError, wantField string) bool {
	for _, violation := range violations {
		if violation.Field == wantField {
			return true
		}
	}
	return false
}

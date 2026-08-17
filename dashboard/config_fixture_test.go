package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestP213ConfigFixtures locks the committed dashboard config fixtures:
// default.yaml must decode and resolve cleanly, while invalid-refresh.yaml
// must be rejected by Resolve with the dashboard.refresh path.
func TestP213ConfigFixtures(t *testing.T) {
	dir := filepath.Join("..", "testdata", "dashboard", "v1", "config")

	validPath := filepath.Join(dir, "default.yaml")
	valid, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatalf("read default config fixture: %v", err)
	}
	validConfig, err := DecodeDashboardConfigFile(valid)
	if err != nil {
		t.Fatalf("decode default config fixture: %v", err)
	}
	if _, err := Resolve(validConfig, nil); err != nil {
		t.Fatalf("resolve default config fixture: %v", err)
	}

	invalidPath := filepath.Join(dir, "invalid-refresh.yaml")
	invalid, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatalf("read invalid-refresh fixture: %v", err)
	}
	invalidConfig, err := DecodeDashboardConfigFile(invalid)
	if err != nil {
		t.Fatalf("decode invalid-refresh fixture: %v", err)
	}
	if _, err := Resolve(invalidConfig, nil); err == nil {
		t.Fatal("invalid-refresh fixture resolved without error")
	} else if !strings.Contains(err.Error(), "dashboard.refresh") {
		t.Fatalf("invalid-refresh error lacks dashboard.refresh path: %v", err)
	}
}

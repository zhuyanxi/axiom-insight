package dashboard

import (
	"strings"
	"testing"
)

// TestDecodeDashboardStrict rejects unknown fields, duplicate keys,
// anchors, aliases, timestamps, non-finite numbers, multiple documents and
// non-string mapping keys, all with the "dashboard." path prefix and no
// echoed values.
func TestDecodeDashboardStrict(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{name: "unknown field", data: "unknown_field: true\n", path: "dashboard.unknown_field"},
		{name: "duplicate key", data: "refresh: 30s\nrefresh: 1m\n", path: "dashboard.refresh"},
		{name: "anchor", data: "refresh: &anchor 30s\n", path: "dashboard.refresh"},
		{name: "alias", data: "refresh: *anchor\n", path: "dashboard"},
		{name: "timestamp", data: "refresh: 2026-08-11\n", path: "dashboard.refresh"},
		{name: "non-finite float", data: "max_panels: .inf\n", path: "dashboard.max_panels"},
		{name: "nan float", data: "max_panels: .nan\n", path: "dashboard.max_panels"},
		{name: "non-string key", data: "123: true\n", path: "dashboard.123"},
		{name: "multiple documents", data: "refresh: 30s\n---\nrefresh: 1m\n", path: "dashboard"},
		{name: "bad scalar type", data: "max_panels: many\n", path: "dashboard.max_panels"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeDashboard([]byte(test.data))
			if err == nil {
				t.Fatalf("DecodeDashboard accepted invalid YAML:\n%s", test.data)
			}
			if !strings.Contains(err.Error(), test.path) {
				t.Errorf("error lacks path %q: %v", test.path, err)
			}
		})
	}
}

// TestDecodeDashboardValid parses a full dashboard node and maps values
// into the typed config.
func TestDecodeDashboardValid(t *testing.T) {
	data := "output_dir: dashboards\nrefresh: 1m\nmax_panels: 42\n"
	config, err := DecodeDashboard([]byte(data))
	if err != nil {
		t.Fatalf("DecodeDashboard failed: %v", err)
	}
	if config == nil {
		t.Fatal("DecodeDashboard returned nil config")
	}
	if config.OutputDir == nil || *config.OutputDir != "dashboards" {
		t.Errorf("OutputDir = %v", config.OutputDir)
	}
	if config.Refresh == nil || *config.Refresh != "1m" {
		t.Errorf("Refresh = %v", config.Refresh)
	}
	if config.MaxPanels == nil || *config.MaxPanels != 42 {
		t.Errorf("MaxPanels = %v", config.MaxPanels)
	}
	// Absent fields stay nil, never defaults: merge decides the value.
	if config.Strict != nil || config.IncludeTraceLinks != nil {
		t.Errorf("absent fields must stay nil, got strict=%v trace=%v", config.Strict, config.IncludeTraceLinks)
	}
}

// TestDecodeDashboardConfigFile extracts the dashboard node from full
// si.yaml contents and leaves other nodes untouched.
func TestDecodeDashboardConfigFile(t *testing.T) {
	data := `name: demo
generation:
  output_dir: generate
dashboard:
  refresh: 5s
`
	config, err := DecodeDashboardConfigFile([]byte(data))
	if err != nil {
		t.Fatalf("DecodeDashboardConfigFile failed: %v", err)
	}
	if config == nil || config.Refresh == nil || *config.Refresh != "5s" {
		t.Fatalf("dashboard node not extracted: %+v", config)
	}
}

// TestDecodeDashboardConfigFileAbsent returns nil when si.yaml has no
// dashboard node.
func TestDecodeDashboardConfigFileAbsent(t *testing.T) {
	config, err := DecodeDashboardConfigFile([]byte("name: demo\n"))
	if err != nil {
		t.Fatalf("DecodeDashboardConfigFile failed: %v", err)
	}
	if config != nil {
		t.Fatalf("expected nil config, got %+v", config)
	}
}

// TestDecodeDashboardConfigFileDuplicateKey rejects repeated dashboard
// nodes across the file.
func TestDecodeDashboardConfigFileDuplicateKey(t *testing.T) {
	data := "dashboard:\n  refresh: 5s\ndashboard:\n  refresh: 1m\n"
	_, err := DecodeDashboardConfigFile([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

// TestDecodeDashboardConfigFileSizeLimit rejects files above the 1 MiB
// ceiling.
func TestDecodeDashboardConfigFileSizeLimit(t *testing.T) {
	large := make([]byte, MaxConfigBytes+1)
	for index := range large {
		large[index] = ' '
	}
	_, err := DecodeDashboardConfigFile(large)
	if err == nil || !strings.Contains(err.Error(), "configuration limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}
}

// TestDecodeDashboardErrorsRedactValues verifies decoder errors never echo
// the rejected payload.
func TestDecodeDashboardErrorsRedactValues(t *testing.T) {
	canary := "s3cr3t-value"
	_, err := DecodeDashboard([]byte("max_panels: " + canary + "\n"))
	if err == nil {
		t.Fatal("DecodeDashboard accepted a non-integer scalar")
	}
	if strings.Contains(err.Error(), canary) {
		t.Errorf("decoder error leaks value %q: %v", canary, err)
	}
}

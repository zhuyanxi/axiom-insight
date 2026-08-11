package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// updateSnapshotEnv switches snapshot regeneration on for this test only.
// Normal test runs never rewrite the committed snapshot.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

// snapshot holds the fully resolved default dashboard policy. Any change
// to the defaults requires an explicit review because the snapshot file is
// committed.
type snapshot struct {
	OutputDir                 string `json:"output_dir"`
	TitleSuffix               string `json:"title_suffix"`
	DatasourceVariableName    string `json:"datasource_variable_name"`
	IncludeTraceLinks         bool   `json:"include_trace_links"`
	IncludeClientDependencies bool   `json:"include_client_dependencies"`
	RateInterval              string `json:"rate_interval"`
	Timezone                  string `json:"timezone"`
	Refresh                   string `json:"refresh"`
	MaxPanels                 int64  `json:"max_panels"`
	MaxQueries                int64  `json:"max_queries"`
	Strict                    bool   `json:"strict"`
	Digest                    string `json:"digest"`
}

func currentSnapshot() snapshot {
	resolved, err := Resolve(nil, nil)
	if err != nil {
		panic(err)
	}
	return snapshot{
		OutputDir:                 resolved.OutputDir,
		TitleSuffix:               resolved.TitleSuffix,
		DatasourceVariableName:    resolved.DatasourceVariableName,
		IncludeTraceLinks:         resolved.IncludeTraceLinks,
		IncludeClientDependencies: resolved.IncludeClientDependencies,
		RateInterval:              resolved.RateInterval,
		Timezone:                  resolved.Timezone,
		Refresh:                   resolved.Refresh,
		MaxPanels:                 resolved.MaxPanels,
		MaxQueries:                resolved.MaxQueries,
		Strict:                    resolved.Strict,
		Digest:                    resolved.Digest(),
	}
}

// TestDefaultPolicySnapshot fixes the default dashboard policy bytes. A
// default change must be reviewed and the snapshot regenerated with
// SI_UPDATE_GOLDEN=1.
func TestDefaultPolicySnapshot(t *testing.T) {
	contents, err := json.MarshalIndent(currentSnapshot(), "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	contents = append(contents, '\n')

	snapshotPath := filepath.Join("testdata", "default_dashboard_policy.json")
	if os.Getenv(updateSnapshotEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, contents, 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		t.Logf("updated snapshot %s", snapshotPath)
		return
	}
	expected, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot %s (set %s=1 to regenerate): %v", snapshotPath, updateSnapshotEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("default policy differs from snapshot %s; set %s=1 to regenerate", snapshotPath, updateSnapshotEnv)
	}
}

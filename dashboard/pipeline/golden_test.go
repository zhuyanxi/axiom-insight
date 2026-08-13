package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// updateSnapshotEnv switches snapshot regeneration on for this package.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

// pipelineGolden pins the deterministic P2-10 output: the definition
// hash, counts, metadata and diagnostics of the composite fixture.
// Regenerate with SI_UPDATE_GOLDEN=1.
type pipelineGolden struct {
	HashVersion string             `json:"hash_version"`
	ServiceName string             `json:"service_name"`
	Title       string             `json:"title"`
	UID         string             `json:"uid"`
	SHA256      string             `json:"sha256"`
	RowCount    int                `json:"row_count"`
	PanelCount  int                `json:"panel_count"`
	QueryCount  int                `json:"query_count"`
	RowTitles   []string           `json:"row_titles"`
	Diagnostics []goldenDiagnostic `json:"diagnostics"`
}

type goldenDiagnostic struct {
	Code     string `json:"code"`
	TargetID string `json:"target_id"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func currentPipelineGolden(t *testing.T) pipelineGolden {
	t.Helper()
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	golden := pipelineGolden{
		HashVersion: dashboard.HashVersion,
		ServiceName: plan.ServiceName(),
		Title:       plan.Title(),
		UID:         plan.UID(),
		SHA256:      result.SHA256,
		RowCount:    result.RowCount,
		PanelCount:  result.PanelCount,
		QueryCount:  result.QueryCount,
		Diagnostics: []goldenDiagnostic{},
	}
	for _, row := range plan.Rows() {
		golden.RowTitles = append(golden.RowTitles, row.Title)
	}
	for _, diagnostic := range plan.Diagnostics() {
		golden.Diagnostics = append(golden.Diagnostics, goldenDiagnostic{
			Code: diagnostic.Code, TargetID: diagnostic.TargetID,
			Field: diagnostic.Field, Message: diagnostic.Message,
		})
	}
	return golden
}

// TestPipelineGolden fixes the deterministic P2-10 output bytes.
// Regenerate with SI_UPDATE_GOLDEN=1.
func TestPipelineGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentPipelineGolden(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "pipeline_golden.json")
	if os.Getenv(updateSnapshotEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, contents, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (set %s=1 to regenerate): %v", goldenPath, updateSnapshotEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("pipeline output differs from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

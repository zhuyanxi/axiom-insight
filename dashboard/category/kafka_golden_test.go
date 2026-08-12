package category

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

func currentKafkaGolden(t *testing.T) categoryGolden {
	t.Helper()
	plan, err := BuildKafka(kafkaCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	golden := categoryGolden{
		HashVersion: dashboard.HashVersion,
		ServiceName: "payment",
		Diagnostics: []goldenDiagnostic{},
	}
	for rowIndex, row := range rows {
		goldenRow := goldenRow{
			Category:    string(plan.Rows[rowIndex].Category),
			Title:       row.Title,
			Description: row.Description,
		}
		for panelIndex, panel := range row.Panels {
			planPanel := plan.Rows[rowIndex].Panels[panelIndex]
			goldenPanel := goldenPanel{
				Purpose:     planPanel.Purpose,
				ItemID:      planPanel.ItemID,
				Title:       panel.Title,
				Description: panel.Description,
				Type:        panel.Type,
				Width:       planPanel.Width,
				Height:      planPanel.Height,
				Unit:        planPanel.Unit,
				NoValue:     planPanel.NoValue,
			}
			for _, link := range panel.Links {
				goldenPanel.Links = append(goldenPanel.Links, goldenLink{Title: link.Title, URL: link.URL})
			}
			for targetIndex, target := range panel.Targets {
				goldenPanel.Targets = append(goldenPanel.Targets, goldenTarget{
					RefID:        target.RefID,
					Kind:         planPanel.Targets[targetIndex].Kind,
					PlanID:       planPanel.Targets[targetIndex].PlanID,
					TargetID:     planPanel.Targets[targetIndex].TargetID,
					Expr:         target.Expr,
					LegendFormat: target.LegendFormat,
				})
			}
			goldenRow.Panels = append(goldenRow.Panels, goldenPanel)
		}
		golden.Rows = append(golden.Rows, goldenRow)
	}
	for _, diagnostic := range plan.Diagnostics {
		golden.Diagnostics = append(golden.Diagnostics, goldenDiagnostic{
			Code: diagnostic.Code, TargetID: diagnostic.TargetID,
			Field: diagnostic.Field, Message: diagnostic.Message,
		})
	}
	return golden
}

// TestKafkaGolden fixes the deterministic P2-08 output bytes: row,
// producer/consumer panels, PromQL, trace links and diagnostics.
// Regenerate with SI_UPDATE_GOLDEN=1.
func TestKafkaGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentKafkaGolden(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "kafka_golden.json")
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
		t.Fatalf("kafka rows differ from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

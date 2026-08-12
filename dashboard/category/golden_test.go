package category

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// updateSnapshotEnv switches snapshot regeneration on for this test only.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

type categoryGolden struct {
	HashVersion string             `json:"hash_version"`
	ServiceName string             `json:"service_name"`
	Rows        []goldenRow        `json:"rows"`
	Diagnostics []goldenDiagnostic `json:"diagnostics"`
}

type goldenRow struct {
	Category    string        `json:"category"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Panels      []goldenPanel `json:"panels"`
}

type goldenPanel struct {
	Purpose     string         `json:"purpose"`
	ItemID      string         `json:"item_id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Type        string         `json:"type"`
	Width       int            `json:"width"`
	Height      int            `json:"height"`
	Unit        string         `json:"unit"`
	NoValue     string         `json:"no_value"`
	Links       []goldenLink   `json:"links,omitempty"`
	Targets     []goldenTarget `json:"targets"`
}

type goldenTarget struct {
	RefID        string `json:"ref_id"`
	Kind         string `json:"kind"`
	PlanID       string `json:"plan_id"`
	TargetID     string `json:"target_id"`
	Expr         string `json:"expr"`
	LegendFormat string `json:"legend_format"`
}

type goldenLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type goldenDiagnostic struct {
	Code     string `json:"code"`
	TargetID string `json:"target_id"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func currentCategoryGolden(t *testing.T) categoryGolden {
	t.Helper()
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
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

// TestCategoryGolden fixes the deterministic P2-07 output bytes: rows,
// panel shapes, rendered PromQL, trace links and diagnostics. Regenerate
// with SI_UPDATE_GOLDEN=1.
func TestCategoryGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentCategoryGolden(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "category_golden.json")
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
		t.Fatalf("category rows differ from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

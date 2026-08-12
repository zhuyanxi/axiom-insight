package overview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// updateSnapshotEnv switches snapshot regeneration on for this test only.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

type overviewGolden struct {
	HashVersion        string             `json:"hash_version"`
	ServiceName        string             `json:"service_name"`
	DatasourceVariable goldenVariable     `json:"datasource_variable"`
	OperationVariable  *goldenVariable    `json:"operation_variable"`
	Panels             []goldenPanel      `json:"panels"`
	Diagnostics        []goldenDiagnostic `json:"diagnostics"`
}

type goldenVariable struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Query      string         `json:"query,omitempty"`
	Hide       *int           `json:"hide,omitempty"`
	Options    []goldenOption `json:"options,omitempty"`
	Current    *goldenCurrent `json:"current,omitempty"`
	Multi      bool           `json:"multi,omitempty"`
	IncludeAll bool           `json:"include_all,omitempty"`
}

type goldenOption struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

type goldenCurrent struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

type goldenPanel struct {
	Purpose     string         `json:"purpose"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Type        string         `json:"type"`
	Width       int            `json:"width"`
	Height      int            `json:"height"`
	Unit        string         `json:"unit"`
	NoValue     string         `json:"no_value"`
	Targets     []goldenTarget `json:"targets"`
}

type goldenTarget struct {
	RefID        string   `json:"ref_id"`
	Kind         string   `json:"kind"`
	Categories   []string `json:"categories"`
	ItemIDs      []string `json:"item_ids"`
	PlanIDs      []string `json:"plan_ids"`
	Expr         string   `json:"expr"`
	LegendFormat string   `json:"legend_format"`
}

type goldenDiagnostic struct {
	Code     string `json:"code"`
	TargetID string `json:"target_id"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func currentOverviewGolden(t *testing.T) overviewGolden {
	t.Helper()
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	variables, panels, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	golden := overviewGolden{
		HashVersion: dashboard.HashVersion,
		ServiceName: "payment",
		DatasourceVariable: goldenVariable{
			Name: variables[0].Name, Type: variables[0].Type,
			Query: variables[0].Query, Hide: variables[0].Hide,
		},
		Diagnostics: []goldenDiagnostic{},
	}
	if len(variables) > 1 && plan.OperationVariable != nil {
		golden.OperationVariable = &goldenVariable{
			Name: plan.OperationVariable.Name, Type: plan.OperationVariable.Type,
			Multi: plan.OperationVariable.Multi, IncludeAll: plan.OperationVariable.IncludeAll,
		}
		for _, option := range plan.OperationVariable.Options {
			golden.OperationVariable.Options = append(golden.OperationVariable.Options,
				goldenOption{Text: option.Text, Value: option.Value})
		}
		if plan.OperationVariable.Current != nil {
			golden.OperationVariable.Current = &goldenCurrent{
				Text: plan.OperationVariable.Current.Text, Value: plan.OperationVariable.Current.Value,
			}
		}
	}
	for index, panel := range plan.Panels {
		goldenPanel := goldenPanel{
			Purpose: panel.Purpose, Title: panel.Title, Description: panel.Description,
			Type: panel.Type, Width: panel.Width, Height: panel.Height,
			Unit: panel.Unit, NoValue: panel.NoValue,
		}
		for targetIndex, target := range panel.Targets {
			categories := make([]string, 0, len(target.Categories))
			for _, category := range target.Categories {
				categories = append(categories, string(category))
			}
			goldenPanel.Targets = append(goldenPanel.Targets, goldenTarget{
				RefID:        panels[index].Targets[targetIndex].RefID,
				Kind:         target.Kind,
				Categories:   categories,
				ItemIDs:      target.ItemIDs,
				PlanIDs:      target.PlanIDs,
				Expr:         target.Expr,
				LegendFormat: target.LegendFormat,
			})
		}
		golden.Panels = append(golden.Panels, goldenPanel)
	}
	for _, diagnostic := range plan.Diagnostics {
		golden.Diagnostics = append(golden.Diagnostics, goldenDiagnostic{
			Code: diagnostic.Code, TargetID: diagnostic.TargetID,
			Field: diagnostic.Field, Message: diagnostic.Message,
		})
	}
	return golden
}

// TestOverviewGolden fixes the deterministic P2-06 output bytes: variables,
// panel shapes, rendered PromQL, family references and diagnostics.
// Regenerate with SI_UPDATE_GOLDEN=1.
func TestOverviewGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentOverviewGolden(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "overview_golden.json")
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
		t.Fatalf("overview differs from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

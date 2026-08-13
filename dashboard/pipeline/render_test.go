package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// TestRenderFull is AC1: the composite plan renders into canonical bytes
// that decode, validate and match the reported hash and counts.
func TestRenderFull(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(result.Bytes) == 0 {
		t.Fatal("rendered bytes are empty")
	}
	sum := sha256.Sum256(result.Bytes)
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 = %q, want %x", result.SHA256, sum)
	}
	rows := plan.Rows()
	if result.RowCount != len(rows) {
		t.Errorf("row count = %d, want %d", result.RowCount, len(rows))
	}
	if result.PanelCount != totalPanels(rows) {
		t.Errorf("panel count = %d, want %d", result.PanelCount, totalPanels(rows))
	}
	if result.QueryCount != totalQueries(rows) {
		t.Errorf("query count = %d, want %d", result.QueryCount, totalQueries(rows))
	}

	decoded, err := model.Decode(result.Bytes)
	if err != nil {
		t.Fatalf("rendered bytes fail strict decode: %v", err)
	}
	if violations := model.Validate(decoded); len(violations) > 0 {
		t.Fatalf("rendered bytes fail validation: %v", violations)
	}
	if decoded.SchemaVersion != model.SchemaVersion || decoded.Title != "payment Observability" {
		t.Errorf("decoded metadata = %d/%q, want %d/%q", decoded.SchemaVersion, decoded.Title, model.SchemaVersion, "payment Observability")
	}
	if decoded.UID != plan.UID() {
		t.Errorf("decoded uid = %q, want %q", decoded.UID, plan.UID())
	}
	if decoded.ID != nil {
		t.Errorf("decoded id must be null, got %v", decoded.ID)
	}
}

// TestRenderCanonical pins the canonical serialization: two-space
// indentation, trailing LF and no HTML escaping.
func TestRenderCanonical(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !bytes.HasSuffix(result.Bytes, []byte("\n")) {
		t.Error("canonical JSON must end with a trailing LF")
	}
	if !bytes.Contains(result.Bytes, []byte("\n  \"title\": \"payment Observability\"")) {
		t.Error("canonical JSON must use two-space indentation")
	}
	if bytes.Contains(result.Bytes, []byte("\\u003c")) {
		t.Error("canonical JSON must not HTML-escape")
	}
}

// TestRenderDeterminism is AC3: rendering twice yields identical bytes
// and hashes.
func TestRenderDeterminism(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	first, err := Render(plan)
	if err != nil {
		t.Fatalf("Render(first): %v", err)
	}
	second, err := Render(plan)
	if err != nil {
		t.Fatalf("Render(second): %v", err)
	}
	if !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("rendered bytes differ across runs")
	}
	if first.SHA256 != second.SHA256 {
		t.Errorf("sha256 differs across runs: %q vs %q", first.SHA256, second.SHA256)
	}
}

// TestRenderInvalidPlan is AC2: a plan with a duplicate panel ID fails
// with DASHBOARD_RENDER_ERROR and no bytes are returned.
func TestRenderInvalidPlan(t *testing.T) {
	plan := &Plan{
		serviceName: "payment",
		title:       "payment Observability",
		uid:         "si-payment-v1",
		timezone:    "browser",
		refresh:     "30s",
		variables: []model.Variable{{
			Name: "datasource", Type: model.VariableTypeDatasource, Query: "prometheus",
		}},
		rows: []model.Row{
			{ID: 1, Title: "HTTP", Panels: []model.Panel{
				{ID: 42, Title: "rate", Type: model.PanelTypeStat, GridPos: model.GridPos{X: 0, Y: 0, W: 6, H: 8},
					Datasource: &model.DatasourceRef{Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable},
					Targets:    []model.Target{{RefID: "A", Expr: "rate(x)", Format: "time_series", Datasource: &model.DatasourceRef{Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable}}},
					FieldConfig: model.FieldConfig{Defaults: model.FieldConfigDefaults{Unit: "ops/s"}},
				},
			}},
			{ID: 2, Title: "RPC", Panels: []model.Panel{
				{ID: 42, Title: "rate", Type: model.PanelTypeStat, GridPos: model.GridPos{X: 0, Y: 0, W: 6, H: 8},
					Datasource: &model.DatasourceRef{Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable},
					Targets:    []model.Target{{RefID: "A", Expr: "rate(y)", Format: "time_series", Datasource: &model.DatasourceRef{Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable}}},
					FieldConfig: model.FieldConfig{Defaults: model.FieldConfigDefaults{Unit: "ops/s"}},
				},
			}},
		},
	}
	result, err := Render(plan)
	if err == nil {
		t.Fatal("Render must fail for a plan with duplicate panel IDs")
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodeRenderError {
		t.Fatalf("error = %v, want DASHBOARD_RENDER_ERROR", err)
	}
	if result != nil {
		t.Fatalf("Render returned a partial result on failure: %+v", result)
	}
}

// TestRenderNilPlan fails cleanly instead of panicking.
func TestRenderNilPlan(t *testing.T) {
	result, err := Render(nil)
	if err == nil {
		t.Fatal("Render(nil) must fail")
	}
	if result != nil {
		t.Fatalf("Render(nil) returned a result: %+v", result)
	}
}

// TestRenderNoSecrets scans the rendered JSON for the forbidden
// generation-time metadata: no digest, path, host, user or timestamp.
func TestRenderNoSecrets(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	document := string(result.Bytes)
	for _, forbidden := range []string{
		plan.PolicyDigest(),
		"/Users/",
		"hostname",
		"timestamp",
		"created",
		"2026-",
	} {
		if strings.Contains(document, forbidden) {
			t.Errorf("rendered dashboard leaks forbidden metadata %q", forbidden)
		}
	}
}

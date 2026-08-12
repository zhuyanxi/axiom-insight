package model

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, relative string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "dashboard", relative)
}

func readFixture(t *testing.T, relative string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, relative))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relative, err)
	}
	return data
}

// TestRoundTripAC1: the full fixture strictly decodes, validates,
// renders canonically and decodes again to a semantically equal model.
func TestRoundTripAC1(t *testing.T) {
	data := readFixture(t, "model/valid/full.json")
	dashboard, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if violations := Validate(dashboard); len(violations) > 0 {
		t.Fatalf("validation: %v", violations)
	}
	rendered, err := Render(dashboard)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	reparsed, err := Decode(rendered)
	if err != nil {
		t.Fatalf("re-decode: %v\n%s", err, rendered)
	}
	if violations := Validate(reparsed); len(violations) > 0 {
		t.Fatalf("re-validation: %v", violations)
	}
	second, err := Render(reparsed)
	if err != nil {
		t.Fatalf("re-render: %v", err)
	}
	if !bytes.Equal(rendered, second) {
		t.Fatal("render is not idempotent")
	}
	// Contract constants are pinned.
	if dashboard.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", dashboard.SchemaVersion, SchemaVersion)
	}
	if dashboard.ID != nil {
		t.Error("id must be null")
	}
	if dashboard.Version != 0 || !dashboard.Editable {
		t.Error("version/editable not pinned")
	}
}

// TestRejectsUnsupportedAC2: unknown fields, duplicate keys, __inputs,
// HTML/plugin panel types and external datasource refs all fail with a
// JSON path.
func TestRejectsUnsupportedAC2(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantError string
	}{
		{"unknown field", "model/invalid/unknown-field.json", "bogus"},
		{"duplicate key", "model/invalid/duplicate-key.json", "duplicate key"},
		{"inputs", "model/invalid/inputs.json", "__inputs"},
		{"html panel", "model/invalid/html-panel.json", "unsupported panel type"},
		{"plugin type", "model/invalid/plugin-type.json", "unsupported panel type"},
		{"external datasource", "model/invalid/external-datasource.json", "datasource"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readFixture(t, test.fixture)
			dashboard, decodeErr := Decode(data)
			if decodeErr == nil {
				violations := Validate(dashboard)
				if len(violations) == 0 {
					t.Fatal("fixture must be rejected")
				}
				if !strings.Contains(violations[0].Error(), test.wantError) {
					t.Fatalf("violation %q lacks %q", violations[0].Error(), test.wantError)
				}
				if !strings.Contains(violations[0].Field, ".") && violations[0].Field != "panels[0].type" {
					t.Errorf("violation lacks a JSON path: %s", violations[0].Field)
				}
				return
			}
			if !strings.Contains(decodeErr.Error(), test.wantError) {
				t.Fatalf("decode error %q lacks %q", decodeErr.Error(), test.wantError)
			}
		})
	}
}

// TestRejectsStructureAC3: duplicate panel IDs, out-of-bounds grids,
// wrong refIds, missing datasource variables, external links and invalid
// variable queries fail semantic validation with a located path.
func TestRejectsStructureAC3(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantField string
	}{
		{"bad grid", "model/invalid/bad-grid.json", "gridPos"},
		{"duplicate panel ID", "model/invalid/duplicate-panel-id.json", "panels[1].id"},
		{"bad refId", "model/invalid/bad-refid.json", "refId"},
		{"external link", "model/invalid/external-link.json", "links[0].url"},
		{"bad variable query", "model/invalid/bad-variable.json", "templating.list[0].query"},
		{"missing datasource variable", "model/invalid/missing-datasource-variable.json", "datasource"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readFixture(t, test.fixture)
			dashboard, err := Decode(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			violations := Validate(dashboard)
			if len(violations) == 0 {
				t.Fatal("fixture must fail validation")
			}
			found := false
			for _, violation := range violations {
				if strings.Contains(violation.Field, test.wantField) {
					found = true
				}
			}
			if !found {
				t.Errorf("violations %v lack field %q", violations, test.wantField)
			}
		})
	}
}

// TestDeterminismAC5: rendering the same model ten times yields
// byte-identical output with no timestamps or random values.
func TestDeterminismAC5(t *testing.T) {
	dashboard, err := Decode(readFixture(t, "model/valid/full.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	first, err := Render(dashboard)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for range 10 {
		rendered, err := Render(dashboard)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !bytes.Equal(rendered, first) {
			t.Fatal("render changed between runs")
		}
	}
	for _, forbidden := range []string{"2026-", "00000000-0000", "hostname"} {
		if strings.Contains(string(first), forbidden) {
			t.Errorf("output contains %q", forbidden)
		}
	}
}

// TestCorpusAC4: every compatibility corpus fixture decodes, validates,
// renders and re-validates without a running Grafana.
func TestCorpusAC4(t *testing.T) {
	entries, err := os.ReadDir(fixturePath(t, "corpus"))
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("corpus has %d entries, want at least 3", len(entries))
	}
	for _, entry := range entries {
		t.Run(entry.Name(), func(t *testing.T) {
			data := readFixture(t, filepath.Join("corpus", entry.Name()))
			dashboard, err := Decode(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if violations := Validate(dashboard); len(violations) > 0 {
				t.Fatalf("validation: %v", violations)
			}
			rendered, err := Render(dashboard)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if _, err := Decode(rendered); err != nil {
				t.Fatalf("re-decode: %v", err)
			}
		})
	}
}

// TestMinimalRoundTrip: the minimal fixture round-trips too.
func TestMinimalRoundTrip(t *testing.T) {
	dashboard, err := Decode(readFixture(t, "model/valid/minimal.json"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if violations := Validate(dashboard); len(violations) > 0 {
		t.Fatalf("validation: %v", violations)
	}
	if _, err := Render(dashboard); err != nil {
		t.Fatalf("render: %v", err)
	}
}

// TestDecodeRejectsSizeAndDepthLimits: oversized documents and deep
// nesting fail deterministically.
func TestDecodeRejectsSizeAndDepthLimits(t *testing.T) {
	if _, err := Decode([]byte("{\"title\":\"" + strings.Repeat("x", MaxDocumentBytes) + "\"}")); err == nil {
		t.Fatal("oversized document must fail")
	}
	depth := strings.Repeat("[", MaxDepth+2) + strings.Repeat("]", MaxDepth+2)
	if _, err := Decode([]byte(depth)); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("deep nesting must fail, got %v", err)
	}
	if _, err := Decode([]byte{0xff, 0xfe, 0x00}); err == nil {
		t.Fatal("invalid UTF-8 must fail")
	}
}

// TestExtendedFieldValidation bounds the P2-06 model extensions: panel
// description length, no-value text length and variable hide range.
func TestExtendedFieldValidation(t *testing.T) {
	base := func() *Dashboard {
		return &Dashboard{
			SchemaVersion: SchemaVersion,
			Title:         "x",
			UID:           "x-uid",
			ID:            nil,
			Version:       0,
			Editable:      true,
			Templating: Templating{List: []Variable{{
				Name: "datasource", Type: VariableTypeDatasource, Query: "prometheus",
			}}},
			Rows: []Row{{
				ID: 1, Title: "row",
				Panels: []Panel{{
					ID: 1, Title: "panel", Type: PanelTypeStat,
					GridPos:     GridPos{X: 0, Y: 0, W: 6, H: 8},
					Datasource:  &DatasourceRef{Type: DatasourceTypePrometheus, UID: DatasourceVariable},
					FieldConfig: FieldConfig{Defaults: FieldConfigDefaults{}},
				}},
			}},
			Annotations: Annotations{},
		}
	}

	long := strings.Repeat("d", MaxDescriptionLength+1)
	tooLongDescription := base()
	tooLongDescription.Rows[0].Panels[0].Description = long
	if !violationAt(Validate(tooLongDescription), "description") {
		t.Error("overlong description accepted")
	}

	tooLongRowDescription := base()
	tooLongRowDescription.Rows[0].Description = long
	if !violationAt(Validate(tooLongRowDescription), "description") {
		t.Error("overlong row description accepted")
	}

	tooLongNoValue := base()
	tooLongNoValue.Rows[0].Panels[0].FieldConfig.Defaults.NoValue = strings.Repeat("0", MaxNoValueLength+1)
	if !violationAt(Validate(tooLongNoValue), "noValue") {
		t.Error("overlong noValue accepted")
	}

	badHide := base()
	hide := 3
	badHide.Templating.List[0].Hide = &hide
	if !violationAt(Validate(badHide), "hide") {
		t.Error("hide=3 accepted")
	}

	validHide := base()
	validHide.Templating.List[0].Hide = new(int)
	if violations := Validate(validHide); len(violations) != 0 {
		t.Errorf("hide=0 rejected: %v", violations)
	}
}

func violationAt(violations []*ValidationError, fieldPart string) bool {
	for _, violation := range violations {
		if strings.Contains(violation.Field, fieldPart) {
			return true
		}
	}
	return false
}

// TestQueryMetadataForms pins the mutually exclusive metadata contract:
// legacy (plan_id+target_id+kind), overview (kind+categories+item_ids+
// plan_ids), never mixed, never with empty references.
func TestQueryMetadataForms(t *testing.T) {
	valid := func() *Dashboard {
		return &Dashboard{
			SchemaVersion: SchemaVersion,
			Title:         "x",
			UID:           "x-uid",
			ID:            nil,
			Version:       0,
			Editable:      true,
			Templating: Templating{List: []Variable{{
				Name: "datasource", Type: VariableTypeDatasource, Query: "prometheus",
			}}},
			Rows: []Row{{
				ID: 1, Title: "row",
				Panels: []Panel{{
					ID: 1, Title: "panel", Type: PanelTypeStat,
					GridPos:     GridPos{X: 0, Y: 0, W: 6, H: 8},
					Datasource:  &DatasourceRef{Type: DatasourceTypePrometheus, UID: DatasourceVariable},
					FieldConfig: FieldConfig{Defaults: FieldConfigDefaults{}},
				}},
			}},
			Annotations: Annotations{},
		}
	}
	withMetadata := func(base *Dashboard, metadata *QueryMetadata) *Dashboard {
		base.Rows[0].Panels[0].Targets = []Target{{
			RefID: "A", Expr: "sum(rate(x[$__rate_interval])) by (operation)",
			Format: "time_series", Metadata: metadata,
		}}
		return base
	}

	legacy := withMetadata(valid(), &QueryMetadata{PlanID: "m", TargetID: "e", Kind: "rate"})
	if violations := Validate(legacy); len(violations) != 0 {
		t.Errorf("legacy metadata rejected: %v", violations)
	}
	overview := withMetadata(valid(), &QueryMetadata{
		Kind: "rate", Categories: []string{"http"}, ItemIDs: []string{"e"}, PlanIDs: []string{"m"},
	})
	if violations := Validate(overview); len(violations) != 0 {
		t.Errorf("overview metadata rejected: %v", violations)
	}

	mixed := withMetadata(valid(), &QueryMetadata{
		PlanID: "m", TargetID: "e", Kind: "rate",
		Categories: []string{"http"}, ItemIDs: []string{"e"}, PlanIDs: []string{"m"},
	})
	if !violationAt(Validate(mixed), "metadata") {
		t.Error("mixed metadata accepted")
	}

	emptyCategory := withMetadata(valid(), &QueryMetadata{
		Kind: "rate", Categories: []string{""}, ItemIDs: []string{"e"}, PlanIDs: []string{"m"},
	})
	if !violationAt(Validate(emptyCategory), "categories") {
		t.Error("empty category reference accepted")
	}
	emptyItem := withMetadata(valid(), &QueryMetadata{
		Kind: "rate", Categories: []string{"http"}, ItemIDs: []string{""}, PlanIDs: []string{"m"},
	})
	if !violationAt(Validate(emptyItem), "item_ids") {
		t.Error("empty item reference accepted")
	}
	emptyPlan := withMetadata(valid(), &QueryMetadata{
		Kind: "rate", Categories: []string{"http"}, ItemIDs: []string{"e"}, PlanIDs: []string{""},
	})
	if !violationAt(Validate(emptyPlan), "plan_ids") {
		t.Error("empty plan reference accepted")
	}
}

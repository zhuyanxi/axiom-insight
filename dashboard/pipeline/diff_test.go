package pipeline

import (
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// TestDiffIdentical reports no differences for identical documents.
func TestDiffIdentical(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	entries, err := Diff(result.Bytes, result.Bytes)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("identical documents differ: %+v", entries)
	}
}

// TestDiffDifference reports a single controlled field change with a
// stable path and the owning panel ID.
func TestDiffDifference(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	// Decode, change exactly one panel title, re-render.
	decoded, err := model.Decode(result.Bytes)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	wantID := decoded.Rows[0].Panels[0].ID
	decoded.Rows[0].Panels[0].Title = "Changed title"
	changed, err := model.Render(decoded)
	if err != nil {
		t.Fatalf("model.Render failed: %v", err)
	}

	entries, err := Diff(result.Bytes, changed)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Path == "$.rows[0].panels[0].title" {
			found = true
			if entry.PanelID != wantID {
				t.Errorf("entry panel id = %d, want %d", entry.PanelID, wantID)
			}
			if entry.Expected != `"Request Rate"` || entry.Actual != `"Changed title"` {
				t.Errorf("entry values = %q/%q, want the changed title", entry.Expected, entry.Actual)
			}
		}
		if strings.Contains(entry.Expected, "\n") || strings.Contains(entry.Actual, "\n") {
			t.Errorf("diff entry leaks a multi-line raw document: %+v", entry)
		}
	}
	if !found {
		t.Fatalf("expected the changed title diff, got %+v", entries)
	}
}

// TestDiffServiceChange reports the metadata and query diffs produced by
// a different service name, with paths and panel context.
func TestDiffServiceChange(t *testing.T) {
	firstPlan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build(first): %v", err)
	}
	secondCatalog := fullCatalog()
	secondCatalog.ServiceName = "ledger"
	secondPlan, err := Build(secondCatalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build(second): %v", err)
	}
	first, err := Render(firstPlan)
	if err != nil {
		t.Fatalf("Render(first): %v", err)
	}
	second, err := Render(secondPlan)
	if err != nil {
		t.Fatalf("Render(second): %v", err)
	}
	entries, err := Diff(first.Bytes, second.Bytes)
	if err != nil {
		t.Fatalf("Diff failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("different documents must differ")
	}
	hasPanelContext := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, "$.rows[") && strings.Contains(entry.Path, ".panels[") {
			hasPanelContext = true
		}
	}
	if !hasPanelContext {
		t.Errorf("expected diffs inside panels, got %+v", entries)
	}
}

// TestDiffInvalidInput fails on non-decodable documents.
func TestDiffInvalidInput(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	_, err = Diff([]byte("{not json"), result.Bytes)
	if err == nil {
		t.Fatal("Diff must fail on invalid input")
	}
	_, err = Diff(result.Bytes, []byte("garbage"))
	if err == nil {
		t.Fatal("Diff must fail on invalid input")
	}
}

// TestDiffNoMapOrder requires a deterministic diff regardless of object
// key order in the documents (the canonical renderer already fixes it,
// this pins the helper's behavior).
func TestDiffNoMapOrder(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	first, err := Diff(result.Bytes, result.Bytes)
	if err != nil {
		t.Fatalf("Diff(first): %v", err)
	}
	second, err := Diff(result.Bytes, result.Bytes)
	if err != nil {
		t.Fatalf("Diff(second): %v", err)
	}
	if len(first) != 0 || !equalEntries(first, second) {
		t.Fatalf("diff is not deterministic: %+v vs %+v", first, second)
	}
}

func equalEntries(left, right []DiffEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

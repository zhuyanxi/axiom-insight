package naming

import (
	"testing"
)

// TestDisambiguateStableSuffixes is AC2: two targets normalizing to the
// same name get deterministic suffixes derived from their own target IDs,
// plus a locatable GEN_NAME_COLLISION diagnostic.
func TestDisambiguateStableSuffixes(t *testing.T) {
	policy := NamingPolicy{}
	items := []NameItem{
		{Signal: "metrics", TargetID: "endpoint:orders-create", Name: "orders_create_requests_total"},
		{Signal: "metrics", TargetID: "endpoint:orders-create-v2", Name: "orders_create_requests_total"},
	}
	results, diagnostics := policy.Disambiguate(items)
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	first := resultFor(results, "endpoint:orders-create")
	second := resultFor(results, "endpoint:orders-create-v2")
	if first == nil || second == nil {
		t.Fatal("missing disambiguation result")
	}
	if first.Suffixed {
		t.Error("smallest target ID must keep the base name")
	}
	if first.Name != "orders_create_requests_total" {
		t.Errorf("base name = %q", first.Name)
	}
	if !second.Suffixed {
		t.Error("second target must be suffixed")
	}
	if second.Name == first.Name {
		t.Fatal("collision was not disambiguated")
	}
	wantSuffix := shortHash("endpoint:orders-create-v2")
	if second.Name != "orders_create_requests_total_"+wantSuffix {
		t.Errorf("suffix = %q, want derived from own target ID", second.Name)
	}
	if len(diagnostics.Items()) != 1 {
		t.Fatalf("diagnostics = %v, want exactly one", diagnostics.Items())
	}
	diagnostic := diagnostics.Items()[0]
	if diagnostic.Code != "GEN_NAME_COLLISION" {
		t.Errorf("code = %q, want GEN_NAME_COLLISION", diagnostic.Code)
	}
	if diagnostic.TargetID != "endpoint:orders-create-v2" {
		t.Errorf("diagnostic target = %q", diagnostic.TargetID)
	}
	if diagnostic.Signal != "metrics" {
		t.Errorf("diagnostic signal = %q", diagnostic.Signal)
	}
}

// TestDisambiguateInputOrderIndependence: permuting the input must not
// change the name mapping (collision determinism requirement).
func TestDisambiguateInputOrderIndependence(t *testing.T) {
	policy := NamingPolicy{}
	base := []NameItem{
		{Signal: "metrics", TargetID: "dep:a", Name: "http_requests_total"},
		{Signal: "metrics", TargetID: "dep:b", Name: "http_requests_total"},
		{Signal: "metrics", TargetID: "dep:c", Name: "http_requests_total"},
		{Signal: "tracing", TargetID: "dep:a", Name: "http_requests_total"},
	}
	permutations := [][]NameItem{
		base,
		{base[3], base[1], base[2], base[0]},
		{base[2], base[0], base[3], base[1]},
	}
	var reference string
	for index, items := range permutations {
		results, _ := policy.Disambiguate(items)
		names := make([]string, 0, len(results))
		for _, result := range results {
			names = append(names, result.Signal+"|"+result.TargetID+"|"+result.Name)
		}
		joined := ""
		for _, name := range names {
			joined += name + ";"
		}
		if index == 0 {
			reference = joined
			continue
		}
		if joined != reference {
			t.Fatalf("permutation %d changed the mapping:\nbase:  %s\nperm:  %s", index, reference, joined)
		}
	}
}

// TestDisambiguateGroupedBySignal: equal names in different signals do
// not collide.
func TestDisambiguateGroupedBySignal(t *testing.T) {
	policy := NamingPolicy{}
	items := []NameItem{
		{Signal: "metrics", TargetID: "dep:a", Name: "same"},
		{Signal: "tracing", TargetID: "dep:b", Name: "same"},
	}
	results, diagnostics := policy.Disambiguate(items)
	if len(diagnostics.Items()) != 0 {
		t.Fatalf("cross-signal names must not collide, got %v", diagnostics.Items())
	}
	for _, result := range results {
		if result.Suffixed {
			t.Errorf("cross-signal name was suffixed: %+v", result)
		}
	}
}

// TestDisambiguateUniqueNamesUntouched: distinct names never change.
func TestDisambiguateUniqueNamesUntouched(t *testing.T) {
	policy := NamingPolicy{}
	items := []NameItem{
		{Signal: "metrics", TargetID: "dep:a", Name: "a"},
		{Signal: "metrics", TargetID: "dep:b", Name: "b"},
	}
	results, diagnostics := policy.Disambiguate(items)
	if len(diagnostics.Items()) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diagnostics.Items())
	}
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	if results[0].Name != "a" || results[1].Name != "b" {
		t.Errorf("names changed without a collision: %v", results)
	}
}

func resultFor(results []NameResult, targetID string) *NameResult {
	for index := range results {
		if results[index].TargetID == targetID {
			return &results[index]
		}
	}
	return nil
}

// TestDisambiguationSuffixIsStable: the exported suffix helper derives
// the same suffix the collision table uses, so planners can match
// disambiguated names back to their items.
func TestDisambiguationSuffixIsStable(t *testing.T) {
	targetID := "dep:orders-v2"
	want := shortHash(targetID)
	if got := DisambiguationSuffix(targetID); got != want {
		t.Errorf("DisambiguationSuffix(%q) = %q, want %q", targetID, got, want)
	}
	if len(DisambiguationSuffix(targetID)) != CollisionSuffixLength {
		t.Errorf("suffix length = %d, want %d", len(DisambiguationSuffix(targetID)), CollisionSuffixLength)
	}
	if DisambiguationSuffix("dep:a") == DisambiguationSuffix("dep:b") {
		t.Error("different target IDs must produce different suffixes")
	}
}

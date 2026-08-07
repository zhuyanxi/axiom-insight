package e2e

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestPermutationInvarianceAC3: at least 20 deterministic random
// permutations of the composite IR must produce byte-identical golden
// files.
func TestPermutationInvarianceAC3(t *testing.T) {
	baseline, baselineOutputs := planAndRender(t, "composite.json", nil)
	if baseline == nil {
		t.Fatal("baseline plan is nil")
	}

	rng := rand.New(rand.NewSource(20260807))
	const permutations = 25
	for iteration := range permutations {
		document := loadIRFixture(t, "composite.json")
		rng.Shuffle(len(document.Functions), func(i, j int) {
			document.Functions[i], document.Functions[j] = document.Functions[j], document.Functions[i]
		})
		rng.Shuffle(len(document.Endpoints), func(i, j int) {
			document.Endpoints[i], document.Endpoints[j] = document.Endpoints[j], document.Endpoints[i]
		})
		rng.Shuffle(len(document.Dependencies), func(i, j int) {
			document.Dependencies[i], document.Dependencies[j] = document.Dependencies[j], document.Dependencies[i]
		})
		rng.Shuffle(len(document.CallEdges), func(i, j int) {
			document.CallEdges[i], document.CallEdges[j] = document.CallEdges[j], document.CallEdges[i]
		})

		_, outputs := planAndRenderPermuted(t, document)
		for name, rendered := range outputs {
			expected := baselineOutputs[name]
			if !bytes.Equal(rendered, expected) {
				t.Fatalf("permutation %d changed %s bytes", iteration, name)
			}
		}
	}
}

// TestDeterminismAC2: the composite renders identically across 10 runs,
// time zones, locales and unrelated environment variables.
func TestDeterminismAC2(t *testing.T) {
	_, baseline := planAndRender(t, "composite.json", nil)
	t.Setenv("TZ", "Pacific/Auckland")
	t.Setenv("LANG", "tr_TR.UTF-8")
	t.Setenv("LC_ALL", "tr_TR.UTF-8")
	t.Setenv("SI_UNRELATED_VARIABLE", "canary-env-value-9f8e")
	for range 10 {
		_, outputs := planAndRender(t, "composite.json", nil)
		for name, rendered := range outputs {
			if !bytes.Equal(rendered, baseline[name]) {
				t.Fatalf("%s changed across environment variations", name)
			}
		}
	}
}

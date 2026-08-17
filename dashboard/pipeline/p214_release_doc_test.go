package pipeline

import (
	"os"
	"strings"
	"testing"
)

// TestP214ReleaseChecklistContract keeps release evidence aligned with the
// executable gates and versioned Dashboard output contract.
func TestP214ReleaseChecklistContract(t *testing.T) {
	contents, err := os.ReadFile("../../docs/phase-2-release-checklist.md")
	if err != nil {
		t.Fatalf("read Phase 2 release checklist: %v", err)
	}
	text := string(contents)
	for _, required := range []string{
		"make phase2-quality",
		"make dashboard-perf",
		"GOPROXY=off GOSUMDB=off",
		"grafana.dashboard/v1",
		"Schema `41`",
		"testdata/dashboard/v1/golden/",
		"testdata/dashboard/corpus/",
		"TestP213DeterminismAndPermutations",
		"TestP213SensitiveCanaryFullChain",
		"TestP214CanonicalOutput",
		"BenchmarkP214Dashboard1000",
		"BenchmarkP214DashboardScanToWrite",
		"single-file atomic replacement",
		"crash-atomic multi-step transaction",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("release checklist missing %q", required)
		}
	}
}

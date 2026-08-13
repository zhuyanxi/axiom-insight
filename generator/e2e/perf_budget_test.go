package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// perfBaseline pins the reference-runner budget for 1,000 pre-constructed
// entities (Plan + three Renderers). Values are recorded per platform from
// a fixed environment: baseline.json is the Apple Silicon reference and
// baseline-linux-amd64.json is the GitHub ubuntu-latest (x86_64) reference;
// enforcement is opt-in via SI_ENFORCE_PERF_BUDGET=1 so local development
// is never flaky. Updates require a documented baseline review.
type perfBaseline struct {
	Entities       int    `json:"entities"`
	MaxNanoseconds int64  `json:"max_nanoseconds"`
	MaxAllocated   uint64 `json:"max_allocated_bytes"`
	// Tolerance is the approved regression margin (0.2 = 20%).
	Tolerance float64 `json:"tolerance"`
}

func loadPerfBaseline(t *testing.T) perfBaseline {
	t.Helper()
	baselineFile := "baseline.json"
	switch runtime.GOOS + "_" + runtime.GOARCH {
	case "linux_amd64":
		baselineFile = "baseline-linux-amd64.json"
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "generator", "perf", baselineFile))
	if err != nil {
		t.Fatalf("read perf baseline %s: %v", baselineFile, err)
	}
	var baseline perfBaseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatalf("parse perf baseline: %v", err)
	}
	return baseline
}

// TestPerformanceBudget1000Entities enforces the approved budget for the
// 1,000-entity pipeline when SI_ENFORCE_PERF_BUDGET=1. It reports the
// stage (plan+render), elapsed time, allocated bytes and the baseline on
// failure, so a regression can be explained before the baseline is
// updated.
func TestPerformanceBudget1000Entities(t *testing.T) {
	if os.Getenv("SI_ENFORCE_PERF_BUDGET") == "" {
		t.Skip("performance budget enforcement is opt-in (SI_ENFORCE_PERF_BUDGET=1)")
	}
	baseline := loadPerfBaseline(t)
	document := benchmarkIR(baseline.Entities)
	overrides := []policy.Overrides{{
		Metrics: policy.MetricsOverrides{
			MaxInstruments:     new(int64(policy.HardMaxInstruments)),
			MaxEstimatedSeries: new(int64(policy.HardMaxEstimatedSeries)),
		},
	}}

	// Warm-up, then three benchmark rounds; the minimum per-operation time
	// is used so a single contended CI run cannot trip the budget.
	_, _ = planAndRenderDocument(t, document, overrides)
	var result testing.BenchmarkResult
	for range 3 {
		roundResult := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				_, _ = planAndRenderDocument(b, document, overrides)
			}
		})
		if result.N == 0 || roundResult.NsPerOp() < result.NsPerOp() {
			result = roundResult
		}
	}

	allowedNS := int64(float64(baseline.MaxNanoseconds) * (1 + baseline.Tolerance))
	allowedBytes := uint64(float64(baseline.MaxAllocated) * (1 + baseline.Tolerance))
	if result.NsPerOp() > allowedNS || uint64(result.AllocedBytesPerOp()) > allowedBytes {
		t.Fatalf(
			"performance budget exceeded for %d entities: stage=plan+render elapsed=%v allocated=%d bytes "+
				"(baseline elapsed=%v allocated=%d bytes tolerance=%.0f%%); update only after a reviewed baseline",
			baseline.Entities, time.Duration(result.NsPerOp()), result.AllocedBytesPerOp(),
			time.Duration(baseline.MaxNanoseconds), baseline.MaxAllocated, baseline.Tolerance*100)
	}
}

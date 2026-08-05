package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// TestGoldenMetricsPlans pins rendered metrics.yaml bytes for the four
// required scenarios: all four types, empty metrics, dynamic fallback and
// a post-collision plan.
func TestGoldenMetricsPlans(t *testing.T) {
	tests := []struct {
		name   string
		plan   func() *observabilityv1.GenerationPlan
		golden string
	}{
		{
			name:   "four types",
			plan:   renderMetricsPlanFixture,
			golden: "golden/metrics_four_types.yaml",
		},
		{
			name: "empty",
			plan: func() *observabilityv1.GenerationPlan {
				return &observabilityv1.GenerationPlan{
					SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
					SourceIrSchemaVersion: "v1",
					ServiceName:           "orders",
				}
			},
			golden: "golden/metrics_empty.yaml",
		},
		{
			name: "dynamic fallback",
			plan: func() *observabilityv1.GenerationPlan {
				plan := renderMetricsPlanFixture()
				// A dynamic target keeps generic definitions and a
				// runtime binding with an explicit fallback.
				plan.Metrics[0].Description = "Generic dependency metrics; target values omitted"
				plan.Metrics[0].Value = runtimeBinding("runtime.operation.duration_seconds")
				return plan
			},
			golden: "golden/metrics_dynamic_fallback.yaml",
		},
		{
			name: "post collision",
			plan: func() *observabilityv1.GenerationPlan {
				plan := renderMetricsPlanFixture()
				// A name that survived P1-04 disambiguation carries the
				// stable suffix; the renderer must keep it verbatim.
				plan.Metrics[0].Name = "orders_redis_get_redis_operations_total_1a2b3c4d"
				return plan
			},
			golden: "golden/metrics_post_collision.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := RenderMetricsPlan(test.plan(), summaryPolicy())
			if err != nil {
				t.Fatalf("RenderMetricsPlan failed: %v", err)
			}
			goldenPath := filepath.Join("testdata", test.golden)
			if os.Getenv(updateGoldenEnv) != "" {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("create golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, rendered, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated golden file %s", goldenPath)
				return
			}
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file %s (set %s=1 to regenerate): %v", goldenPath, updateGoldenEnv, err)
			}
			if !bytes.Equal(rendered, expected) {
				t.Fatalf("rendered output differs from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
			}
		})
	}
}

// TestGoldenMetricsPlansRoundTrip: every golden file strictly decodes and
// re-renders to identical bytes.
func TestGoldenMetricsPlansRoundTrip(t *testing.T) {
	goldens := []string{
		"golden/metrics_four_types.yaml",
		"golden/metrics_empty.yaml",
		"golden/metrics_dynamic_fallback.yaml",
		"golden/metrics_post_collision.yaml",
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatalf("read golden %s: %v", name, err)
			}
			document, err := DecodeMetrics(data)
			if err != nil {
				t.Fatalf("golden %s does not decode strictly: %v", name, err)
			}
			if violations := document.Validate(); len(violations) > 0 {
				t.Fatalf("golden %s fails semantic validation: %v", name, violations)
			}
			rendered, err := RenderMetrics(document)
			if err != nil {
				t.Fatalf("golden %s does not re-render: %v", name, err)
			}
			if !bytes.Equal(rendered, data) {
				t.Fatalf("golden %s is not round-trip stable", name)
			}
		})
	}
}

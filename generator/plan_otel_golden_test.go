package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/proto"
)

// TestGoldenOTelPlans pins rendered otel.yaml bytes for the full
// five-kind plan and the minimal single-root plan.
func TestGoldenOTelPlans(t *testing.T) {
	tests := []struct {
		name   string
		plan   func() *observabilityv1.GenerationPlan
		golden string
	}{
		{
			name:   "full five kinds",
			plan:   otelPlanFixture,
			golden: "golden/otel_full.yaml",
		},
		{
			name: "minimal root",
			plan: func() *observabilityv1.GenerationPlan {
				plan := otelPlanFixture()
				plan.Spans = plan.Spans[:1]
				return plan
			},
			golden: "golden/otel_minimal.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := RenderTracingPlan(test.plan(), defaultPolicyValue())
			if err != nil {
				t.Fatalf("RenderTracingPlan failed: %v", err)
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

// TestGoldenOTelPlansRoundTrip: every golden file strictly decodes and
// re-renders to identical bytes.
func TestGoldenOTelPlansRoundTrip(t *testing.T) {
	goldens := []string{
		"golden/otel_full.yaml",
		"golden/otel_minimal.yaml",
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatalf("read golden %s: %v", name, err)
			}
			document, err := DecodeOTel(data)
			if err != nil {
				t.Fatalf("golden %s does not decode strictly: %v", name, err)
			}
			if violations := document.Validate(); len(violations) > 0 {
				t.Fatalf("golden %s fails semantic validation: %v", name, violations)
			}
			rendered, err := RenderOTel(document)
			if err != nil {
				t.Fatalf("golden %s does not re-render: %v", name, err)
			}
			if !bytes.Equal(rendered, data) {
				t.Fatalf("golden %s is not round-trip stable", name)
			}
		})
	}
}

func protoClone(span *observabilityv1.SpanPlan) *observabilityv1.SpanPlan {
	return proto.Clone(span).(*observabilityv1.SpanPlan)
}

// BenchmarkRenderTracingPlan measures pure render time and allocations
// for a plan with 1,000 spans. Results are recorded, not gated.
func BenchmarkRenderTracingPlan(b *testing.B) {
	plan := otelPlanFixture()
	for len(plan.Spans) < 1000 {
		span := protoClone(plan.Spans[0])
		index := itoa(len(plan.Spans))
		span.Id = "tracing:ep:" + index + ":root"
		span.Name = "GET /items/" + index
		for _, event := range span.Events {
			event.Id = span.Id + ":exception"
		}
		plan.Spans = append(plan.Spans, span)
	}
	policy := defaultPolicyValue()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		contents, err := RenderTracingPlan(plan, policy)
		if err != nil {
			b.Fatalf("RenderTracingPlan failed: %v", err)
		}
		if len(contents) == 0 {
			b.Fatal("empty render output")
		}
	}
}

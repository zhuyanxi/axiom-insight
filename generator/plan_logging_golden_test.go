package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/proto"
)

// TestGoldenLoggingPlans pins rendered logging.yaml bytes for the default
// full file, the start-enabled file and the minimal single-event file.
func TestGoldenLoggingPlans(t *testing.T) {
	tests := []struct {
		name   string
		plan   func() *observabilityv1.GenerationPlan
		golden string
	}{
		{
			name:   "default full",
			plan:   loggingPlanFixture,
			golden: "golden/logging_default.yaml",
		},
		{
			name: "start enabled",
			plan: func() *observabilityv1.GenerationPlan {
				plan := loggingPlanFixture()
				started := proto.Clone(plan.Logs[0]).(*observabilityv1.LogPlan)
				started.Id = "logging:ep:orders:started"
				started.EventName = "http.request.started"
				started.Severity = observabilityv1.LogSeverity_LOG_SEVERITY_INFO
				started.Statuses = nil
				started.Fields = started.Fields[:7] // timestamp..version only
				plan.Logs = append(plan.Logs, started)
				return plan
			},
			golden: "golden/logging_start.yaml",
		},
		{
			name: "minimal",
			plan: func() *observabilityv1.GenerationPlan {
				plan := loggingPlanFixture()
				plan.Logs = plan.Logs[:1]
				return plan
			},
			golden: "golden/logging_minimal.yaml",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := RenderLoggingPlan(test.plan(), defaultPolicyValue())
			if err != nil {
				t.Fatalf("RenderLoggingPlan failed: %v", err)
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

// TestGoldenLoggingPlansRoundTrip: every golden file strictly decodes and
// re-renders to identical bytes.
func TestGoldenLoggingPlansRoundTrip(t *testing.T) {
	goldens := []string{
		"golden/logging_default.yaml",
		"golden/logging_start.yaml",
		"golden/logging_minimal.yaml",
	}
	for _, name := range goldens {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatalf("read golden %s: %v", name, err)
			}
			document, err := DecodeLogging(data)
			if err != nil {
				t.Fatalf("golden %s does not decode strictly: %v", name, err)
			}
			if violations := document.Validate(); len(violations) > 0 {
				t.Fatalf("golden %s fails semantic validation: %v", name, violations)
			}
			rendered, err := RenderLogging(document)
			if err != nil {
				t.Fatalf("golden %s does not re-render: %v", name, err)
			}
			if !bytes.Equal(rendered, data) {
				t.Fatalf("golden %s is not round-trip stable", name)
			}
		})
	}
}

// BenchmarkRenderLoggingPlan measures pure render time and allocations
// for a plan with 1,000 log events. Results are recorded, not gated.
func BenchmarkRenderLoggingPlan(b *testing.B) {
	plan := loggingPlanFixture()
	for len(plan.Logs) < 1000 {
		event := proto.Clone(plan.Logs[0]).(*observabilityv1.LogPlan)
		index := itoa(len(plan.Logs))
		event.Id = "logging:ep:" + index + ":failed"
		event.EventName = "http.request.failed." + index
		event.Severity = observabilityv1.LogSeverity_LOG_SEVERITY_ERROR
		event.Target.Id = "ep:" + index
		plan.Logs = append(plan.Logs, event)
	}
	policy := defaultPolicyValue()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		contents, err := RenderLoggingPlan(plan, policy)
		if err != nil {
			b.Fatalf("RenderLoggingPlan failed: %v", err)
		}
		if len(contents) == 0 {
			b.Fatal("empty render output")
		}
	}
}

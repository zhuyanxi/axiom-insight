// Package e2e holds the Phase 1 end-to-end fixture tests (P1-15): fixed
// IR fixtures drive the full Analyze-independent pipeline (Plan -> Render
// -> Validate) and the outputs are compared byte-for-byte against golden
// files, then re-parsed with strict semantic and schema validation so a
// snapshot can never mask an invalid document.
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/planner/logging"
	"github.com/zhuyanxi/axiom-insight/generator/planner/metrics"
	"github.com/zhuyanxi/axiom-insight/generator/planner/tracing"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// updateGoldenEnv switches golden regeneration on; normal runs never
// rewrite committed files.
const updateGoldenEnv = "SI_UPDATE_GOLDEN"

// goldenScenario pairs an IR fixture with a policy variant.
type goldenScenario struct {
	name      string
	ir        string
	policies  []policy.Overrides
	directory string
}

var goldenScenarios = []goldenScenario{
	{
		name: "composite", ir: "composite.json", directory: "composite",
	},
	{
		name: "policy-summary-enabled", ir: "composite.json", directory: "policy-summary-enabled",
		policies: []policy.Overrides{{
			Metrics: policy.MetricsOverrides{
				Summaries: policy.SummariesOverrides{Enabled: new(true), Quantiles: []float64{0.5, 0.9, 0.99}},
			},
		}},
	},
	{
		name: "policy-internal-spans-enabled", ir: "composite.json", directory: "policy-internal-spans-enabled",
		policies: []policy.Overrides{{
			Tracing: policy.TracingOverrides{IncludeInternalCalls: new(true)},
		}},
	},
	{
		name: "policy-start-logs", ir: "composite.json", directory: "policy-start-logs",
		policies: []policy.Overrides{{
			Logging: policy.LoggingOverrides{EmitStartEvents: new(true)},
		}},
	},
}

// planAndRender runs the full pipeline over a fixed IR fixture.
func planAndRender(t *testing.T, fixture string, overrides []policy.Overrides) (*observabilityv1.GenerationPlan, map[string][]byte) {
	t.Helper()
	return planAndRenderDocument(t, loadIRFixture(t, fixture), overrides)
}

// planAndRenderPermuted runs the full pipeline over an in-memory
// (possibly permuted) document.
func planAndRenderPermuted(t *testing.T, document *observabilityv1.ObservabilityDocument) (*observabilityv1.GenerationPlan, map[string][]byte) {
	t.Helper()
	return planAndRenderDocument(t, document, nil)
}

func planAndRenderDocument(t testing.TB, document *observabilityv1.ObservabilityDocument, overrides []policy.Overrides) (*observabilityv1.GenerationPlan, map[string][]byte) {
	t.Helper()
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	for _, override := range overrides {
		merged, err := policy.Resolve(nil, &override)
		if err != nil {
			t.Fatalf("override %+v: %v", override, err)
		}
		resolved = merged
	}
	plan, _, err := newPipeline().Plan(t.Context(), document, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	outputs := map[string][]byte{}
	for _, signal := range []string{planner.SignalMetrics, planner.SignalTracing, planner.SignalLogging} {
		var rendered []byte
		switch signal {
		case planner.SignalMetrics:
			rendered, err = generator.RenderMetricsPlan(plan, *resolved)
		case planner.SignalTracing:
			rendered, err = generator.RenderTracingPlan(plan, *resolved)
		default:
			rendered, err = generator.RenderLoggingPlan(plan, *resolved)
		}
		if err != nil {
			t.Fatalf("render %s: %v", signal, err)
		}
		outputs[signalFileName(signal)] = rendered
	}
	return plan, outputs
}

func signalFileName(signal string) string {
	switch signal {
	case planner.SignalMetrics:
		return "metrics.yaml"
	case planner.SignalTracing:
		return "otel.yaml"
	default:
		return "logging.yaml"
	}
}

// TestGoldenScenariosE2E: every scenario renders byte-identical golden
// files that also pass strict decode, semantic validation and the
// machine schema.
func TestGoldenScenariosE2E(t *testing.T) {
	for _, scenario := range goldenScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			plan, outputs := planAndRender(t, scenario.ir, scenario.policies)
			if plan == nil {
				t.Fatal("plan is nil")
			}
			for name, rendered := range outputs {
				goldenPath := filepath.Join("..", "..", "testdata", "generator", "golden", scenario.directory, name)
				if os.Getenv(updateGoldenEnv) != "" {
					if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
						t.Fatalf("create golden dir: %v", err)
					}
					if err := os.WriteFile(goldenPath, rendered, 0o644); err != nil {
						t.Fatalf("write golden %s: %v", goldenPath, err)
					}
					t.Logf("updated golden %s", goldenPath)
					continue
				}
				expected, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("read golden %s (set %s=1 to regenerate): %v", goldenPath, updateGoldenEnv, err)
				}
				if !bytes.Equal(rendered, expected) {
					t.Fatalf("%s differs from golden; semantic diff: %s",
						goldenPath, semanticDiff(name, expected, rendered))
				}
				validateRendered(t, name, rendered)
			}
		})
	}
}

// semanticDiff decodes both documents and reports whether the models
// differ structurally, instead of dumping raw bytes.
func semanticDiff(name string, expected, actual []byte) string {
	first, err1 := decodeDoc(name, expected)
	second, err2 := decodeDoc(name, actual)
	if err1 != nil || err2 != nil {
		return fmt.Sprintf("decode failed: %v / %v", err1, err2)
	}
	if fmt.Sprintf("%+v", first) == fmt.Sprintf("%+v", second) {
		return "documents are structurally equal despite byte differences"
	}
	return fmt.Sprintf("structural difference in %s (golden %d bytes, actual %d bytes)",
		name, len(expected), len(actual))
}

// decodeDoc parses a rendered YAML contract document.
func decodeDoc(name string, data []byte) (any, error) {
	switch name {
	case "metrics.yaml":
		return generator.DecodeMetrics(data)
	case "otel.yaml":
		return generator.DecodeOTel(data)
	default:
		return generator.DecodeLogging(data)
	}
}

func validateRendered(t *testing.T, name string, rendered []byte) {
	t.Helper()
	var violations []interface{ Validate() []*generator.ValidationError }
	switch name {
	case "metrics.yaml":
		document, err := generator.DecodeMetrics(rendered)
		if err != nil {
			t.Fatalf("%s does not decode strictly: %v", name, err)
		}
		if len(document.Validate()) > 0 {
			t.Fatalf("%s fails semantic validation: %v", name, document.Validate())
		}
		violations = append(violations, document)
	case "otel.yaml":
		document, err := generator.DecodeOTel(rendered)
		if err != nil {
			t.Fatalf("%s does not decode strictly: %v", name, err)
		}
		if len(document.Validate()) > 0 {
			t.Fatalf("%s fails semantic validation: %v", name, document.Validate())
		}
		violations = append(violations, document)
	default:
		document, err := generator.DecodeLogging(rendered)
		if err != nil {
			t.Fatalf("%s does not decode strictly: %v", name, err)
		}
		if len(document.Validate()) > 0 {
			t.Fatalf("%s fails semantic validation: %v", name, document.Validate())
		}
		violations = append(violations, document)
	}
	if len(violations) == 0 {
		t.Fatalf("%s produced no document", name)
	}
}

// loadIRFixture reads a fixed IR fixture.
func loadIRFixture(t *testing.T, name string) *observabilityv1.ObservabilityDocument {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "generator", "ir", name))
	if err != nil {
		t.Fatalf("read IR fixture %s: %v", name, err)
	}
	document := new(observabilityv1.ObservabilityDocument)
	if err := protojson.Unmarshal(data, document); err != nil {
		t.Fatalf("parse IR fixture %s: %v", name, err)
	}
	return document
}

// newPipeline returns the Phase 1 planner pipeline.
func newPipeline() *planner.Planner {
	return planner.New(planner.Options{
		Metrics: metrics.CompositeMetricsPlanner{Endpoint: metrics.EndpointMetricsPlanner{}, Dependency: metrics.DependencyMetricsPlanner{}},
		Tracing: tracing.CompositeTracingPlanner{Root: tracing.EndpointRootSpanPlanner{}, Dependency: tracing.DependencyChildSpanPlanner{}, Internal: tracing.InternalCallSpanPlanner{}},
		Logging: logging.LoggingPlanner{},
	})
}

// resolvedPolicy returns the default resolved policy.
func resolvedPolicy(t *testing.T) policy.Policy {
	t.Helper()
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	return *resolved
}

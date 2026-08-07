package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// capabilityMatrix lists every Phase 0 endpoint/dependency kind and the
// fixture that must exercise it (AC1).
var capabilityMatrix = []struct {
	kind    string
	fixture string
	check   func(*testing.T, *observabilityv1.GenerationPlan)
}{
	{"HTTP handler", "composite.json", checkEndpoint("ep:http")},
	{"gRPC handler", "composite.json", checkEndpoint("ep:grpc")},
	{"Cron job", "composite.json", checkEndpoint("ep:cron")},
	{"Kafka producer", "composite.json", checkDependency("dep:producer")},
	{"Kafka consumer", "composite.json", checkDependency("dep:consumer")},
	{"SQL", "composite.json", checkDependency("dep:sql")},
	{"Redis", "composite.json", checkDependency("dep:redis")},
	{"HTTP client", "composite.json", checkDependency("dep:http-client")},
	{"RPC client", "composite.json", checkDependency("dep:rpc-client")},
}

// TestCapabilityMatrixAC1: every Phase 0 kind has a positive Plan and
// signal-level assertion.
func TestCapabilityMatrixAC1(t *testing.T) {
	plan, outputs := planAndRender(t, "composite.json", nil)
	for _, entry := range capabilityMatrix {
		t.Run(entry.kind, func(t *testing.T) {
			entry.check(t, plan)
		})
	}
	if len(outputs) != 3 {
		t.Fatalf("expected three rendered signals, got %d", len(outputs))
	}
	for name, rendered := range outputs {
		if len(rendered) == 0 {
			t.Errorf("%s rendered empty", name)
		}
	}
}

func checkEndpoint(id string) func(*testing.T, *observabilityv1.GenerationPlan) {
	return func(t *testing.T, plan *observabilityv1.GenerationPlan) {
		t.Helper()
		found := false
		for _, metric := range plan.GetMetrics() {
			if metric.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("endpoint %s has no metric plan", id)
		}
		found = false
		for _, span := range plan.GetSpans() {
			if span.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("endpoint %s has no span plan", id)
		}
		found = false
		for _, event := range plan.GetLogs() {
			if event.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("endpoint %s has no log plan", id)
		}
	}
}

func checkDependency(id string) func(*testing.T, *observabilityv1.GenerationPlan) {
	return func(t *testing.T, plan *observabilityv1.GenerationPlan) {
		t.Helper()
		found := false
		for _, metric := range plan.GetMetrics() {
			if metric.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("dependency %s has no metric plan", id)
		}
		found = false
		for _, span := range plan.GetSpans() {
			if span.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("dependency %s has no span plan", id)
		}
		found = false
		for _, event := range plan.GetLogs() {
			if event.GetTarget().GetId() == id {
				found = true
			}
		}
		if !found {
			t.Errorf("dependency %s has no failed-event plan", id)
		}
	}
}

// TestFixtureInventory: every IR fixture and every golden file referenced
// by the matrix exists on disk.
func TestFixtureInventory(t *testing.T) {
	irDir := filepath.Join("..", "..", "testdata", "generator", "ir")
	for _, name := range []string{
		"composite.json", "dynamic-targets.json", "naming-collisions.json",
		"sensitive-values.json", "invalid-references.json",
	} {
		if _, err := os.Stat(filepath.Join(irDir, name)); err != nil {
			t.Errorf("IR fixture %s missing: %v", name, err)
		}
	}
	goldenRoot := filepath.Join("..", "..", "testdata", "generator", "golden")
	for _, scenario := range goldenScenarios {
		for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
			path := filepath.Join(goldenRoot, scenario.directory, name)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("golden %s missing: %v", path, err)
			}
		}
	}
}

// TestInvalidReferenceFatal: the invalid-references fixture fails the
// plan with a dangling reference and never produces a plan.
func TestInvalidReferenceFatal(t *testing.T) {
	document := loadIRFixture(t, "invalid-references.json")
	plan, err := planFatal(t, document)
	if err == nil {
		t.Fatal("invalid references must fail planning")
	}
	if !strings.Contains(err.Error(), "GEN_DANGLING_REFERENCE") {
		t.Errorf("error %q lacks GEN_DANGLING_REFERENCE", err.Error())
	}
	if plan != nil {
		t.Fatal("no plan may be returned for invalid references")
	}
}

// planFatal runs the pipeline and returns its error without failing the
// test on the expected failure.
func planFatal(t *testing.T, document *observabilityv1.ObservabilityDocument) (*observabilityv1.GenerationPlan, error) {
	t.Helper()
	plan, _, err := newPipeline().Plan(t.Context(), document, resolvedPolicy(t))
	return plan, err
}

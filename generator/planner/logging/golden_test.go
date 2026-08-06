package logging

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// updateGoldenEnv switches golden-file regeneration on for this test
// only, matching the repository-wide convention.
const updateGoldenEnv = "SI_UPDATE_GOLDEN"

// TestGoldenLogPlans pins the deterministic bytes of the composite log
// plan (endpoints with start events enabled plus all six dependencies).
func TestGoldenLogPlans(t *testing.T) {
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Logging: &policy.LoggingConfig{EmitStartEvents: new(true)},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalLogging}
	plannerInstance := planner.New(planner.Options{Logging: LoggingPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), loggingDocument(), *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	contents := marshalGolden(t, plan)

	goldenPath := filepath.Join("testdata", "golden", "log_plans.json")
	if os.Getenv(updateGoldenEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, contents, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden file %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s (set %s=1 to regenerate): %v", goldenPath, updateGoldenEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("log plans differ from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
	}
}

// TestLogPlanInputOrderInvariance: reordering entities never changes the
// planned bytes.
func TestLogPlanInputOrderInvariance(t *testing.T) {
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalLogging}
	plannerInstance := planner.New(planner.Options{Logging: LoggingPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), loggingDocument(), *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	reference := string(marshalGolden(t, plan))

	reordered := loggingDocument()
	reordered.Endpoints[0], reordered.Endpoints[2] = reordered.Endpoints[2], reordered.Endpoints[0]
	reordered.Dependencies[0], reordered.Dependencies[5] = reordered.Dependencies[5], reordered.Dependencies[0]
	plan, _, err = plannerInstance.Plan(context.Background(), reordered, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if got := string(marshalGolden(t, plan)); got != reference {
		t.Fatal("input order changed the planned bytes")
	}
}

// TestLoggingPlannerConcurrent: the stateless planner is safe for
// concurrent planning of different documents.
func TestLoggingPlannerConcurrent(t *testing.T) {
	plannerInstance := planner.New(planner.Options{Logging: LoggingPlanner{}})
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalLogging}
	done := make(chan error, 8)
	for index := range 8 {
		go func(index int) {
			document := loggingDocument()
			document.Service.Name = "service-" + itoa(index)
			_, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
			done <- err
		}(index)
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Errorf("concurrent planning failed: %v", err)
		}
	}
}

// marshalGolden serializes a plan to stable golden bytes. protojson's
// indented output contains a detrand-seeded extra space after "key:" that
// varies per compiled binary; re-marshaling through encoding/json removes
// that build dependence so golden files are byte-stable everywhere.
func marshalGolden(t *testing.T, plan *observabilityv1.GenerationPlan) []byte {
	t.Helper()
	contents, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	var value any
	if err := json.Unmarshal(contents, &value); err != nil {
		t.Fatalf("unmarshal plan JSON: %v", err)
	}
	normalized, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("normalize plan JSON: %v", err)
	}
	return append(normalized, '\n')
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}

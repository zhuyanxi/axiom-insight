package tracing

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

// updateGoldenEnv switches golden-file regeneration on for this test
// only, matching the repository-wide convention.
const updateGoldenEnv = "SI_UPDATE_GOLDEN"

// compositeRootSpanDocument covers all three endpoint kinds.
func compositeRootSpanDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:http", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
			{Id: "fn:grpc", QualifiedName: "checkout.OrdersServer", PackagePath: "internal/orders"},
			{Id: "fn:cron", QualifiedName: "checkout.Cleanup", PackagePath: "internal/maintenance"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{
				Id: "ep:orders", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:http",
				HttpMethod: "POST", HttpPath: "/orders/{id}",
			},
			{
				Id: "ep:grpc", Kind: observabilityv1.EndpointKind_GRPC_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:grpc",
				GrpcService: "OrderService", GrpcMethod: "CreateOrder",
			},
			{
				Id: "ep:cron", Kind: observabilityv1.EndpointKind_CRON_JOB,
				Name: "Nightly Cleanup", FunctionId: "fn:cron", CronSchedule: "0 3 * * *",
			},
		},
	}
}

func defaultPolicy() policy.Policy {
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		panic(err)
	}
	return *resolved
}

// TestGoldenRootSpans pins the deterministic bytes of the composite root
// span plan.
func TestGoldenRootSpans(t *testing.T) {
	resolved := defaultPolicy()
	resolved.Signals = []string{planner.SignalTracing}
	plannerInstance := planner.New(planner.Options{Tracing: EndpointRootSpanPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), compositeRootSpanDocument(), resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	contents := marshalGolden(t, plan)

	goldenPath := filepath.Join("testdata", "golden", "root_spans.json")
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
		t.Fatalf("root spans differ from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
	}
}

// TestRootSpanInputOrderInvariance: reordering endpoints never changes
// the planned bytes.
func TestRootSpanInputOrderInvariance(t *testing.T) {
	resolved := defaultPolicy()
	resolved.Signals = []string{planner.SignalTracing}
	plannerInstance := planner.New(planner.Options{Tracing: EndpointRootSpanPlanner{}})

	plan, _, err := plannerInstance.Plan(context.Background(), compositeRootSpanDocument(), resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	reference := marshalRootSpans(t, plan)

	reordered := compositeRootSpanDocument()
	reordered.Endpoints[0], reordered.Endpoints[2] = reordered.Endpoints[2], reordered.Endpoints[0]
	plan, _, err = plannerInstance.Plan(context.Background(), reordered, resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if got := marshalRootSpans(t, plan); got != reference {
		t.Fatal("endpoint order changed the planned bytes")
	}
}

// TestRootSpanPlannerConcurrent: the stateless planner is safe for
// concurrent planning of different documents.
func TestRootSpanPlannerConcurrent(t *testing.T) {
	plannerInstance := planner.New(planner.Options{Tracing: EndpointRootSpanPlanner{}})
	resolved := defaultPolicy()
	resolved.Signals = []string{planner.SignalTracing}
	done := make(chan error, 8)
	for index := range 8 {
		go func(index int) {
			document := compositeRootSpanDocument()
			document.Service.Name = "service-" + itoa(index)
			_, _, err := plannerInstance.Plan(context.Background(), document, resolved)
			done <- err
		}(index)
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Errorf("concurrent planning failed: %v", err)
		}
	}
}

func marshalRootSpans(t *testing.T, plan *observabilityv1.GenerationPlan) string {
	t.Helper()
	contents, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return string(contents)
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

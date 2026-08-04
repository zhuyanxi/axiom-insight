package metrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// updateGoldenEnv switches golden-file regeneration on for this test
// only, matching the repository-wide convention.
const updateGoldenEnv = "SI_UPDATE_GOLDEN"

// TestGoldenEndpointMetrics pins the deterministic bytes of the composite
// endpoint metrics plan (AC6 output determinism at the byte level).
func TestGoldenEndpointMetrics(t *testing.T) {
	resolved, err := policyResolveDefault()
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{Metrics: EndpointMetricsPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), compositeEndpointDocument(), *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	contents, err := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "golden", "endpoint_metrics.json")
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
		t.Fatalf("endpoint metrics differ from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
	}
}

// TestInputOrderInvarianceAC6: reordering the endpoint list never changes
// the planned bytes.
func TestInputOrderInvarianceAC6(t *testing.T) {
	document := compositeEndpointDocument()
	resolved, err := policyResolveDefault()
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{Metrics: EndpointMetricsPlanner{}})

	plan, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	reference := marshalPlan(t, plan)

	reordered := compositeEndpointDocument()
	reordered.Endpoints[0], reordered.Endpoints[2] = reordered.Endpoints[2], reordered.Endpoints[0]
	reordered.Functions[0], reordered.Functions[1] = reordered.Functions[1], reordered.Functions[0]
	plan, _, err = plannerInstance.Plan(context.Background(), reordered, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if got := marshalPlan(t, plan); got != reference {
		t.Fatal("endpoint order changed the planned bytes")
	}
}

// TestEndpointMetricsPlannerConcurrent: the stateless planner is safe for
// concurrent planning of different documents.
func TestEndpointMetricsPlannerConcurrent(t *testing.T) {
	plannerInstance := planner.New(planner.Options{Metrics: EndpointMetricsPlanner{}})
	resolved, err := policyResolveDefault()
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	done := make(chan error, 8)
	for index := range 8 {
		go func(index int) {
			document := compositeEndpointDocument()
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

func policyResolveDefault() (*policy.Policy, error) {
	return policy.Resolve(nil, nil)
}

func compositeEndpointDocument() *observabilityv1.ObservabilityDocument {
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

func marshalPlan(t *testing.T, plan *observabilityv1.GenerationPlan) string {
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

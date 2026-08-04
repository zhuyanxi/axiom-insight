package planner

import (
	"context"
	"math/rand"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestPlanPermutationInvariance is AC5: two semantically identical IRs
// with different entity orderings produce identical plan bytes and
// identical report counts.
func TestPlanPermutationInvariance(t *testing.T) {
	planner, _, _, _ := newTestPlanner()
	base := permutableDocument()
	reference, referenceReport, err := planner.Plan(context.Background(), base, defaultPolicy())
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	referenceBytes := marshalDeterministic(t, reference)
	referenceSummary := referenceReport.String()

	rng := rand.New(rand.NewSource(42))
	for iteration := 0; iteration < 5; iteration++ {
		permuted := permutableDocument()
		rng.Shuffle(len(permuted.Functions), func(i, j int) {
			permuted.Functions[i], permuted.Functions[j] = permuted.Functions[j], permuted.Functions[i]
		})
		rng.Shuffle(len(permuted.Endpoints), func(i, j int) {
			permuted.Endpoints[i], permuted.Endpoints[j] = permuted.Endpoints[j], permuted.Endpoints[i]
		})
		rng.Shuffle(len(permuted.Dependencies), func(i, j int) {
			permuted.Dependencies[i], permuted.Dependencies[j] = permuted.Dependencies[j], permuted.Dependencies[i]
		})
		rng.Shuffle(len(permuted.CallEdges), func(i, j int) {
			permuted.CallEdges[i], permuted.CallEdges[j] = permuted.CallEdges[j], permuted.CallEdges[i]
		})
		plan, report, err := planner.Plan(context.Background(), permuted, defaultPolicy())
		if err != nil {
			t.Fatalf("permutation %d failed: %v", iteration, err)
		}
		if got := marshalDeterministic(t, plan); got != referenceBytes {
			t.Fatalf("permutation %d changed plan bytes", iteration)
		}
		if report.String() != referenceSummary {
			t.Fatalf("permutation %d changed the report:\nbase:  %s\nperm:  %s",
				iteration, referenceSummary, report.String())
		}
	}
}

// permutableDocument is a larger composite IR with multiple entities per
// type, so permutations actually reorder items.
func permutableDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "shop"},
		Functions: []*observabilityv1.Function{
			{
				Id: "fn:b", QualifiedName: "shop.B",
				InputEndpointIds: []string{"ep:orders", "ep:cart"},
				DependencyIds:    []string{"dep:sql", "dep:redis"},
				CalleeFunctionIds: []string{"fn:c"},
			},
			{
				Id: "fn:a", QualifiedName: "shop.A",
				InputEndpointIds:  []string{"ep:cart"},
				DependencyIds:     []string{"dep:redis"},
				CallerFunctionIds: []string{"fn:b"},
			},
			{Id: "fn:c", QualifiedName: "shop.C"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{
				Id: "ep:orders", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:b",
				HttpMethod: "POST", HttpPath: "/orders",
			},
			{
				Id: "ep:cart", Kind: observabilityv1.EndpointKind_GRPC_HANDLER,
				Name: "GetCart", FunctionId: "fn:a",
				GrpcService: "CartService", GrpcMethod: "GetCart",
			},
			{
				Id: "ep:cleanup", Kind: observabilityv1.EndpointKind_CRON_JOB,
				Name: "Cleanup", FunctionId: "fn:c", CronSchedule: "0 3 * * *",
			},
		},
		Dependencies: []*observabilityv1.Dependency{
			{Id: "dep:sql", Kind: observabilityv1.DependencyKind_SQL, Name: "Store", FunctionId: "fn:b", Operation: "exec"},
			{Id: "dep:redis", Kind: observabilityv1.DependencyKind_REDIS, Name: "Cache", FunctionId: "fn:a", Operation: "get"},
		},
		CallEdges: []*observabilityv1.CallEdge{
			{Id: "edge:b-a", CallerFunctionId: "fn:b", CalleeFunctionId: "fn:a", Resolution: observabilityv1.CallResolution_RESOLVED},
			{Id: "edge:b-c", CallerFunctionId: "fn:b", CalleeFunctionId: "fn:c", Resolution: observabilityv1.CallResolution_RESOLVED},
		},
	}
}

func marshalDeterministic(t *testing.T, plan *observabilityv1.GenerationPlan) string {
	t.Helper()
	contents, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	return string(contents)
}

package planner

import (
	"context"
	"fmt"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// BenchmarkPlan records planning time and allocations for 100, 1,000 and
// 10,000 IR entities. Results are recorded, not gated: no uncalibrated
// absolute threshold blocks the first merge.
func BenchmarkPlan(b *testing.B) {
	for _, entityCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entities_%d", entityCount), func(b *testing.B) {
			document := benchmarkDocument(entityCount)
			planner, _, _, _ := newTestPlanner()
			policy := defaultPolicy()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				plan, _, err := planner.Plan(context.Background(), document, policy)
				if err != nil {
					b.Fatalf("Plan failed: %v", err)
				}
				if plan == nil {
					b.Fatal("plan is nil")
				}
			}
		})
	}
}

// benchmarkDocument builds an IR with entityCount functions, endpoints,
// dependencies and call edges.
func benchmarkDocument(entityCount int) *observabilityv1.ObservabilityDocument {
	document := &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "bench"},
	}
	for index := 0; index < entityCount; index++ {
		functionID := "fn:" + itoa(index)
		document.Functions = append(document.Functions, &observabilityv1.Function{
			Id: functionID, QualifiedName: "bench.Fn" + itoa(index),
			InputEndpointIds: []string{"ep:" + itoa(index)},
			DependencyIds:    []string{"dep:" + itoa(index)},
		})
		document.Endpoints = append(document.Endpoints, &observabilityv1.Endpoint{
			Id: "ep:" + itoa(index), Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
			Name: "Endpoint" + itoa(index), FunctionId: functionID,
			HttpMethod: "GET", HttpPath: "/items/" + itoa(index),
		})
		document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
			Id: "dep:" + itoa(index), Kind: observabilityv1.DependencyKind_SQL,
			Name: "Store" + itoa(index), FunctionId: functionID, Operation: "exec",
		})
		if index > 0 {
			document.CallEdges = append(document.CallEdges, &observabilityv1.CallEdge{
				Id: "edge:" + itoa(index), CallerFunctionId: "fn:" + itoa(index),
				CalleeFunctionId: "fn:" + itoa(index-1), Resolution: observabilityv1.CallResolution_RESOLVED,
			})
		}
	}
	return document
}

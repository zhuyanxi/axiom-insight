package e2e

import (
	"fmt"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// BenchmarkGenerateFromIR measures Plan + three Renderers over
// pre-constructed IR with 100, 1,000 and 10,000 entities. The Analyzer is
// deliberately excluded so the numbers isolate the Generator pipeline
// (P1-16 budget: 1,000 entities under 1 s and 64 MiB on the reference
// runner, enforced when SI_ENFORCE_PERF_BUDGET=1).
func BenchmarkGenerateFromIR(b *testing.B) {
	for _, entityCount := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprintf("entities_%d", entityCount), func(b *testing.B) {
			document := benchmarkIR(entityCount)
			// Raise the instrument and series budgets so the benchmark
			// measures the pipeline, not the (working) cardinality guard.
			overrides := []policy.Overrides{{
				Metrics: policy.MetricsOverrides{
					MaxInstruments:     new(int64(policy.HardMaxInstruments)),
					MaxEstimatedSeries: new(int64(policy.HardMaxEstimatedSeries)),
				},
			}}
			plan, outputs := planAndRenderDocument(b, document, overrides)
			if plan == nil || len(outputs) != 3 {
				b.Fatal("pipeline produced no output")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = planAndRenderDocument(b, document, overrides)
			}
		})
	}
}

// benchmarkIR builds an IR with entityCount functions, endpoints,
// dependencies and call edges.
func benchmarkIR(entityCount int) *observabilityv1.ObservabilityDocument {
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
			ValueIsStatic: true,
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

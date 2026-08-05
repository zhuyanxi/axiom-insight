package tracing

import (
	"context"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// internalCallDocument builds a function graph with a resolved edge, an
// unresolved edge and a self-recursive edge.
func internalCallDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:handler", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
			{Id: "fn:validator", QualifiedName: "checkout.Validate", PackagePath: "internal/orders"},
		},
		CallEdges: []*observabilityv1.CallEdge{
			{
				Id: "edge:handler-validator", CallerFunctionId: "fn:handler",
				CalleeFunctionId: "fn:validator", Resolution: observabilityv1.CallResolution_RESOLVED,
			},
			{
				Id: "edge:handler-unresolved", CallerFunctionId: "fn:handler",
				Resolution: observabilityv1.CallResolution_UNRESOLVED,
			},
			{
				Id: "edge:handler-self", CallerFunctionId: "fn:handler",
				CalleeFunctionId: "fn:handler", Resolution: observabilityv1.CallResolution_RESOLVED,
			},
		},
	}
}

func planInternalCalls(t *testing.T, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) *planner.TracingResult {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	result, err := InternalCallSpanPlanner{}.PlanTracing(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: resolved,
	})
	if err != nil {
		t.Fatalf("PlanTracing failed: %v", err)
	}
	return result
}

// TestInternalSpansDefaultOffAC3: internal spans are absent by default
// and appear only when the policy enables them.
func TestInternalSpansDefaultOffAC3(t *testing.T) {
	document := internalCallDocument()
	defaults, _ := policy.Resolve(nil, nil)
	off := planInternalCalls(t, document, *defaults)
	if len(off.Items) != 0 {
		t.Fatalf("internal spans must be off by default, got %d", len(off.Items))
	}

	enabled, _ := policy.Resolve(&policy.GenerationConfig{
		Tracing: &policy.TracingConfig{IncludeInternalCalls: new(true)},
	}, nil)
	on := planInternalCalls(t, document, *enabled)
	// Two resolved edges (one normal, one self-recursive) -> two spans;
	// the unresolved edge is skipped with a diagnostic.
	if len(on.Items) != 2 {
		t.Fatalf("span count = %d, want 2", len(on.Items))
	}
	for _, span := range on.Items {
		if span.GetKind() != observabilityv1.SpanKind_SPAN_KIND_INTERNAL {
			t.Errorf("span %s kind = %v", span.GetId(), span.GetKind())
		}
		if span.GetTarget().GetKind() != observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE {
			t.Errorf("span %s target kind = %v", span.GetId(), span.GetTarget().GetKind())
		}
		if span.GetParent().GetMode() != observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT {
			t.Errorf("span %s parent = %v", span.GetId(), span.GetParent().GetMode())
		}
	}
	if on.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 (unresolved edge)", on.Skipped)
	}
	found := false
	for _, diagnostic := range on.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget && diagnostic.TargetID == "edge:handler-unresolved" {
			found = true
		}
	}
	if !found {
		t.Errorf("unresolved edge must produce a capability diagnostic: %v", on.Diagnostics)
	}
}

// TestRecursionTerminatesAC4: self and mutual recursion produce at most
// one plan per static edge; planning terminates.
func TestRecursionTerminatesAC4(t *testing.T) {
	document := internalCallDocument()
	// Mutual recursion: validator -> handler.
	document.CallEdges = append(document.CallEdges, &observabilityv1.CallEdge{
		Id: "edge:validator-handler", CallerFunctionId: "fn:validator",
		CalleeFunctionId: "fn:handler", Resolution: observabilityv1.CallResolution_RESOLVED,
	})
	enabled, _ := policy.Resolve(&policy.GenerationConfig{
		Tracing: &policy.TracingConfig{IncludeInternalCalls: new(true)},
	}, nil)
	result := planInternalCalls(t, document, *enabled)
	// Three resolved edges total (handler->validator, handler->self,
	// validator->handler): exactly one span per edge.
	if len(result.Items) != 3 {
		t.Fatalf("span count = %d, want 3 (one per static edge)", len(result.Items))
	}
	seen := map[string]bool{}
	for _, span := range result.Items {
		if seen[span.GetTarget().GetId()] {
			t.Errorf("duplicate plan for edge %s", span.GetTarget().GetId())
		}
		seen[span.GetTarget().GetId()] = true
	}
}

// TestInternalSpanNameAndAttributes: the internal span name is the
// callee's qualified function name with code attributes.
func TestInternalSpanNameAndAttributes(t *testing.T) {
	document := internalCallDocument()
	enabled, _ := policy.Resolve(&policy.GenerationConfig{
		Tracing: &policy.TracingConfig{IncludeInternalCalls: new(true)},
	}, nil)
	result := planInternalCalls(t, document, *enabled)
	var validatorSpan *observabilityv1.SpanPlan
	for _, span := range result.Items {
		if span.GetTarget().GetId() == "edge:handler-validator" {
			validatorSpan = span
		}
	}
	if validatorSpan == nil {
		t.Fatal("missing handler->validator span")
	}
	if validatorSpan.GetName() != "checkout.Validate" {
		t.Errorf("name = %q, want callee qualified name", validatorSpan.GetName())
	}
	if spanAttr(validatorSpan, "code.function") == nil {
		t.Error("internal span must carry code.function")
	}
}

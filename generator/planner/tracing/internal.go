package tracing

import (
	"context"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// InternalCallSpanPlanner plans INTERNAL child spans for resolved
// internal call edges when the policy enables them. Each static edge
// yields at most one plan — self-recursion and mutual recursion produce
// exactly one span per edge, never an expansion of the call graph.
// Unresolved edges are ignored with a capability diagnostic.
type InternalCallSpanPlanner struct{}

// PlanTracing implements planner.TracingPlanner for internal call edges.
// When include_internal_calls is disabled the result is empty.
func (InternalCallSpanPlanner) PlanTracing(ctx context.Context, input *planner.SignalInput) (*planner.TracingResult, error) {
	result := &planner.TracingResult{}
	if !input.Policy.Tracing.IncludeInternalCalls {
		return result, nil
	}
	serviceName := input.Document.GetService().GetName()
	for _, edge := range input.Document.CallEdges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if edge.GetResolution() != observabilityv1.CallResolution_RESOLVED {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeIncompleteTarget, Signal: planner.SignalTracing,
				TargetID: edge.GetId(), Field: "resolution",
				Message: "call edge is unresolved; internal span omitted",
			})
			result.Skipped++
			continue
		}
		callee := input.Index.Function(edge.GetCalleeFunctionId())
		name := "internal"
		if callee != nil && callee.GetQualifiedName() != "" {
			name = callee.GetQualifiedName()
		}
		span := &observabilityv1.SpanPlan{
			Id:   planner.StableID(planner.SignalTracing, edge.GetId(), planner.PurposeChild),
			Name: name,
			Kind: observabilityv1.SpanKind_SPAN_KIND_INTERNAL,
			Target: &observabilityv1.TargetRef{
				Kind: observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE, Id: edge.GetId(),
			},
			StartTrigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_START},
			EndTrigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
			Parent: &observabilityv1.ParentStrategy{
				Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT,
			},
			Status: fullStatusMapping(),
			Attributes: []*observabilityv1.AttributeBinding{
				serviceNameAttribute(serviceName),
				serviceVersionAttribute(),
			},
		}
		if callee != nil {
			if callee.GetPackagePath() != "" {
				span.Attributes = append(span.Attributes, irStringAttribute("code.namespace", "function.package_path"))
			}
			if callee.GetQualifiedName() != "" {
				span.Attributes = append(span.Attributes, irStringAttribute("code.function", "function.qualified_name"))
			}
		}
		attachEvents(span, edge.GetId(), input.Policy)
		result.Items = append(result.Items, span)
	}

	disambiguateNames(result)

	sort.Slice(result.Items, func(left, right int) bool {
		return result.Items[left].GetId() < result.Items[right].GetId()
	})
	for _, span := range result.Items {
		sort.Slice(span.Attributes, func(left, right int) bool {
			return span.Attributes[left].GetKey() < span.Attributes[right].GetKey()
		})
		sort.Slice(span.Events, func(left, right int) bool {
			return span.Events[left].GetId() < span.Events[right].GetId()
		})
	}
	return result, nil
}

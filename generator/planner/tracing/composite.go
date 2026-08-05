package tracing

import (
	"context"

	"github.com/zhuyanxi/axiom-insight/generator/planner"
)

// CompositeTracingPlanner runs the root span, dependency child span and
// internal call planners and merges their results into one
// TracingPlanner covering the whole tracing signal.
type CompositeTracingPlanner struct {
	Root       EndpointRootSpanPlanner
	Dependency DependencyChildSpanPlanner
	Internal   InternalCallSpanPlanner
}

// PlanTracing implements planner.TracingPlanner.
func (composite CompositeTracingPlanner) PlanTracing(ctx context.Context, input *planner.SignalInput) (*planner.TracingResult, error) {
	roots, err := composite.Root.PlanTracing(ctx, input)
	if err != nil {
		return nil, err
	}
	children, err := composite.Dependency.PlanTracing(ctx, input)
	if err != nil {
		return nil, err
	}
	internals, err := composite.Internal.PlanTracing(ctx, input)
	if err != nil {
		return nil, err
	}
	return &planner.TracingResult{
		Items:       append(append(roots.Items, children.Items...), internals.Items...),
		Diagnostics: append(append(roots.Diagnostics, children.Diagnostics...), internals.Diagnostics...),
		Skipped:     roots.Skipped + children.Skipped + internals.Skipped,
	}, nil
}

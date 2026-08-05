package metrics

import (
	"context"

	"github.com/zhuyanxi/axiom-insight/generator/planner"
)

// CompositeMetricsPlanner runs the endpoint and dependency metrics
// planners and merges their results into one MetricsPlanner covering the
// whole metrics signal. The planner pipeline sorts the merged items and
// enforces the combined instrument/series budget.
type CompositeMetricsPlanner struct {
	Endpoint   EndpointMetricsPlanner
	Dependency DependencyMetricsPlanner
}

// PlanMetrics implements planner.MetricsPlanner.
func (composite CompositeMetricsPlanner) PlanMetrics(ctx context.Context, input *planner.SignalInput) (*planner.MetricsResult, error) {
	endpoints, err := composite.Endpoint.PlanMetrics(ctx, input)
	if err != nil {
		return nil, err
	}
	dependencies, err := composite.Dependency.PlanMetrics(ctx, input)
	if err != nil {
		return nil, err
	}
	return &planner.MetricsResult{
		Items:       append(endpoints.Items, dependencies.Items...),
		Diagnostics: append(endpoints.Diagnostics, dependencies.Diagnostics...),
		Skipped:     endpoints.Skipped + dependencies.Skipped,
	}, nil
}

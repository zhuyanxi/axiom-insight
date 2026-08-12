package query

import (
	"slices"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// ValidateOverviewPlan verifies one P2-06 overview query against the
// catalog item set it aggregates (AC1: every overview target passes the
// P2-05 validator). The rules are the P2-05 rules applied across items:
//
//   - every selector metric name is declared by every aggregated item with
//     the same type and the same label schema (different metric name or
//     label schemas are never merged into one selector);
//   - every plan ID is declared by at least one aggregated item, and the
//     plan IDs cover every selected metric (traceability AC1);
//   - operation matchers are absent (overview queries aggregate all
//     operations); top-failing error selectors may carry the fixed
//     status matcher;
//   - all matcher, function, interval and ratio rules of the P2-05
//     validator hold unchanged.
func ValidateOverviewPlan(plan *QueryPlan, items []dashboard.DashboardItem, serviceName string) []*ValidationError {
	if plan == nil {
		return []*ValidationError{{Field: "expr", Message: "plan is nil"}}
	}
	if len(items) == 0 {
		return []*ValidationError{{Field: "expr", Message: "overview query aggregates no items"}}
	}

	declared := make([]itemDeclarations, 0, len(items))
	unionPlanIDs := make(map[string]bool)
	planIDMetricNames := make(map[string]map[string]bool)
	for _, item := range items {
		declaration := itemDeclarations{
			types:  make(map[string]string, len(item.Metrics)),
			labels: make(map[string][]string, len(item.Metrics)),
		}
		for _, reference := range item.Metrics {
			declaration.types[reference.Name] = reference.Type
			declaration.labels[reference.Name] = reference.Attributes
			unionPlanIDs[reference.PlanID] = true
			if planIDMetricNames[reference.PlanID] == nil {
				planIDMetricNames[reference.PlanID] = make(map[string]bool)
			}
			planIDMetricNames[reference.PlanID][reference.Name] = true
		}
		declared = append(declared, declaration)
	}

	var violations []*ValidationError
	emit := func(field, message string) {
		violations = append(violations, &ValidationError{Field: field, Message: message})
	}
	for _, planID := range plan.PlanIDs {
		if !unionPlanIDs[planID] {
			emit("plan_ids", "references an undeclared plan ID")
		}
	}

	// Every selector must be provable from every aggregated item with the
	// same type and label schema.
	selectors := collectSelectors(plan.Expression)
	if len(selectors) == 0 {
		emit("expr", "query contains no metric selector")
	}
	mergedTypes := make(map[string]string)
	mergedLabels := make(map[string][]string)
	for _, selector := range selectors {
		if selector == nil {
			emit("expr.selectors", "selector is nil")
			continue
		}
		if !metricNamePattern.MatchString(selector.MetricName) {
			emit("expr.selectors.metric", "metric name is not a controlled machine name")
			continue
		}
		schema := declared[0].labels[selector.MetricName]
		metricType := declared[0].types[selector.MetricName]
		if metricType == "" {
			emit("expr.selectors.metric", "metric is not declared by the aggregated items")
			continue
		}
		for _, declaration := range declared[1:] {
			if declaration.types[selector.MetricName] != metricType ||
				!slices.Equal(declaration.labels[selector.MetricName], schema) {
				emit("expr.selectors.metric", "metric type or label schema differs across aggregated items")
				break
			}
		}
		mergedTypes[selector.MetricName] = metricType
		mergedLabels[selector.MetricName] = schema
		covered := false
		for _, planID := range plan.PlanIDs {
			if planIDMetricNames[planID][selector.MetricName] {
				covered = true
				break
			}
		}
		if !covered {
			emit("plan_ids", "plan IDs do not cover a selected metric")
		}
		for _, matcher := range selector.Matchers {
			if matcher.Label == "operation" {
				emit("expr.selectors.matchers.operation", "overview queries must not filter by operation")
			}
		}
	}

	context := &validateContext{
		metricTypes:  mergedTypes,
		metricLabels: mergedLabels,
		operation:    "",
		serviceName:  serviceName,
		inErrorRatio: plan.Kind == QueryKindErrorRatio,
		inFlight:     plan.Kind == QueryKindInFlight,
	}
	if plan.Kind == QueryKindTopFailing {
		context.statusMatcherAllowed = true
	}
	if _, bare := plan.Expression.(*MetricSelector); bare {
		emit("expr", "bare selector is not a valid query root")
	}
	validateExpression(&violations, context, plan.Expression, "expr", 0)
	return violations
}

type itemDeclarations struct {
	types  map[string]string
	labels map[string][]string
}

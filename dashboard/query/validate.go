package query

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// ValidationError is one query-validation violation. Field locates the
// violation inside the expression, e.g. "expr.selectors[0].matchers[1]".
// Messages never echo rejected values.
type ValidationError struct {
	// Field is the violation location.
	Field string
	// Message explains the rule.
	Message string
}

// Error implements error.
func (violation *ValidationError) Error() string {
	return fmt.Sprintf("query: validate: %s: %s", violation.Field, violation.Message)
}

// labelVocabulary is the closed set of label names a generated query may
// use. Dynamic label names are rejected (P2-05 task 7).
var labelVocabulary = map[string]bool{
	"service":   true,
	"operation": true,
	"status":    true,
	"le":        true,
}

// matcherValuePattern bounds service and generic matcher values: ASCII
// letters, digits, underscore, dot and hyphen only (dotted service names
// are legitimate). Quotes, braces, commas, colons, slashes, backslashes,
// whitespace and operator characters are rejected so no value can inject
// matcher syntax.
var matcherValuePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// operationValuePattern bounds operation matcher values to machine-name
// shape: ASCII letters, digits, underscore and hyphen. Dots, colons and
// slashes are never legitimate in a normalized operation.
var operationValuePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// metricNamePattern bounds metric names to the Prometheus charset; a
// metric name never carries quotes or operator characters.
var metricNamePattern = regexp.MustCompile(`^[A-Za-z_:][A-Za-z0-9_:]{0,199}$`)

// spanNamePattern bounds trace-link span names: letters, digits,
// underscores, dots, slashes, spaces and hyphens; no quotes, braces or
// control characters.
var spanNamePattern = regexp.MustCompile(`^[A-Za-z0-9_. /-]{1,128}$`)

// ValidatePlan verifies a generated query against the catalog item it was
// built from (AC1/AC7 cross-check). Every selector metric name must be an
// exact declared MetricPlan name, every matcher label must belong to the
// vocabulary, matcher values must be controlled, functions must match the
// metric type, and bare selectors, dynamic labels and raw regexes are
// rejected. The item is never modified.
func ValidatePlan(plan *QueryPlan, item *dashboard.DashboardItem, serviceName string) []*ValidationError {
	if plan == nil {
		return []*ValidationError{{Field: "expr", Message: "plan is nil"}}
	}
	if item == nil {
		return []*ValidationError{{Field: "expr", Message: "item is nil"}}
	}
	metricTypes := make(map[string]string, len(item.Metrics))
	metricLabels := make(map[string][]string, len(item.Metrics))
	metricPlanIDs := make(map[string]bool, len(item.Metrics))
	for _, reference := range item.Metrics {
		metricTypes[reference.Name] = reference.Type
		metricLabels[reference.Name] = reference.Attributes
		metricPlanIDs[reference.PlanID] = true
	}
	spanPlanIDs := make(map[string]bool, len(item.Spans))
	for _, reference := range item.Spans {
		spanPlanIDs[reference.PlanID] = true
	}
	for _, planID := range plan.PlanIDs {
		if !metricPlanIDs[planID] && !spanPlanIDs[planID] {
			return []*ValidationError{{Field: "plan_ids", Message: "references an undeclared plan ID"}}
		}
	}

	context := &validateContext{
		metricTypes:  metricTypes,
		metricLabels: metricLabels,
		operation:    item.Operation,
		serviceName:  serviceName,
		inErrorRatio: plan.Kind == QueryKindErrorRatio,
		inFlight:     plan.Kind == QueryKindInFlight,
	}
	var violations []*ValidationError
	// A bare selector is never a valid query root: every query aggregates,
	// rates or divides (task 7).
	if _, bare := plan.Expression.(*MetricSelector); bare {
		violations = append(violations, &ValidationError{
			Field: "expr", Message: "bare selector is not a valid query root",
		})
	}
	validateExpression(&violations, context, plan.Expression, "expr", 0)
	return violations
}

type validateContext struct {
	metricTypes  map[string]string
	metricLabels map[string][]string
	operation    string
	serviceName  string
	inErrorRatio bool
	inFlight     bool
	// errorRatioSide is 1 on the numerator side, 2 on the denominator
	// side of an error-ratio expression; 0 elsewhere.
	errorRatioSide int
}

func validateExpression(violations *[]*ValidationError, context *validateContext, expression Expression, field string, depth int) {
	if depth > 8 {
		*violations = append(*violations, &ValidationError{Field: field, Message: "expression is nested too deeply"})
		return
	}
	switch node := expression.(type) {
	case *MetricSelector:
		validateSelector(violations, context, node, field)
	case *RateExpression:
		validateSelector(violations, context, node.Selector, field+".selector")
		if node.Interval != dashboard.DefaultRateInterval {
			*violations = append(*violations, &ValidationError{
				Field: field + ".interval", Message: "rate interval must be the fixed controlled rate interval",
			})
		}
		metricType := context.metricTypes[node.Selector.MetricName]
		if metricType == "gauge" {
			*violations = append(*violations, &ValidationError{
				Field: field, Message: "rate() is not allowed on a gauge",
			})
		}
	case *Aggregation:
		for _, label := range node.By {
			if !labelVocabulary[label] {
				*violations = append(*violations, &ValidationError{
					Field: field + ".by", Message: "group label is outside the controlled vocabulary",
				})
			}
		}
		validateExpression(violations, context, node.Expr, field+".expr", depth+1)
	case *HistogramQuantileExpression:
		if !containsQuantile(FixedQuantiles, node.Quantile) {
			*violations = append(*violations, &ValidationError{
				Field: field + ".quantile", Message: "quantile is outside the fixed set",
			})
		}
		validateExpression(violations, context, node.Expr, field+".expr", depth+1)
	case *BinaryExpression:
		// Error ratio contract (AC5): both sides share the same metric and
		// base matchers; the left side may only add the fixed error-status
		// matcher.
		if context.inErrorRatio {
			leftSelectors, rightSelectors := collectSelectors(node.Left), collectSelectors(node.Right)
			if !sameSelectorDomain(leftSelectors, rightSelectors) {
				*violations = append(*violations, &ValidationError{
					Field: field, Message: "error ratio sides must share the same metric and domain matchers",
				})
			}
			leftContext := *context
			leftContext.errorRatioSide = 1
			rightContext := *context
			rightContext.errorRatioSide = 2
			validateExpression(violations, &leftContext, node.Left, field+".left", depth+1)
			validateExpression(violations, &rightContext, node.Right, field+".right", depth+1)
			return
		}
		validateExpression(violations, context, node.Left, field+".left", depth+1)
		validateExpression(violations, context, node.Right, field+".right", depth+1)
	case *ScalarExpression:
		// A scalar denominator is legal only on the right side of a ratio.
		if field == "expr" {
			*violations = append(*violations, &ValidationError{
				Field: field, Message: "bare scalar is not a valid query",
			})
		}
	default:
		*violations = append(*violations, &ValidationError{
			Field: field, Message: "unsupported expression node",
		})
	}
}

func validateSelector(violations *[]*ValidationError, context *validateContext, selector *MetricSelector, field string) {
	if selector == nil {
		*violations = append(*violations, &ValidationError{Field: field, Message: "selector is nil"})
		return
	}
	if !metricNamePattern.MatchString(selector.MetricName) {
		*violations = append(*violations, &ValidationError{
			Field: field + ".metric", Message: "metric name is not a controlled machine name",
		})
		return
	}
	if context.metricTypes[selector.MetricName] == "" {
		*violations = append(*violations, &ValidationError{
			Field: field + ".metric", Message: "metric is not declared by the item's metric plans",
		})
	}
	for index, matcher := range selector.Matchers {
		matcherField := fmt.Sprintf("%s.matchers[%d]", field, index)
		if !labelVocabulary[matcher.Label] {
			*violations = append(*violations, &ValidationError{
				Field: matcherField + ".label", Message: "matcher label is outside the controlled vocabulary",
			})
		}
		if matcher.Label == "le" {
			// The bucket-boundary matcher is fully fixed: exactly
			// le!="+Inf". It is exempt from the value charset because
			// "+Inf" is the fixed bucket value, not user content.
			if matcher.Op != MatchNotEqual || matcher.Value != rateIntervalLeMatcher {
				*violations = append(*violations, &ValidationError{
					Field: matcherField, Message: "the le matcher must exclude exactly the +Inf bucket",
				})
			}
			continue
		}
		if matcher.Op == MatchRegex {
			if matcher.Value != ErrorStatusPattern {
				*violations = append(*violations, &ValidationError{
					Field: matcherField + ".value", Message: "raw regexes are not allowed; only the fixed error-status pattern",
				})
			}
		} else if !matcherValuePattern.MatchString(matcher.Value) {
			*violations = append(*violations, &ValidationError{
				Field: matcherField + ".value", Message: "matcher value is not a controlled value",
			})
		}
		if matcher.Label == "status" && !(context.inErrorRatio && context.errorRatioSide == 1) {
			*violations = append(*violations, &ValidationError{
				Field: matcherField, Message: "status matchers are only allowed in the error-ratio numerator",
			})
		}
		if context.inFlight && matcher.Label == "status" {
			*violations = append(*violations, &ValidationError{
				Field: matcherField, Message: "in-flight queries must not filter by status",
			})
		}
		if matcher.Label == "service" && matcher.Op == MatchEqual && matcher.Value != context.serviceName {
			*violations = append(*violations, &ValidationError{
				Field: matcherField + ".value", Message: "service matcher must equal the validated service name",
			})
		}
		if matcher.Label == "operation" && matcher.Op == MatchEqual && context.operation != "" && matcher.Value != context.operation {
			*violations = append(*violations, &ValidationError{
				Field: matcherField + ".value", Message: "operation matcher must equal the validated item operation",
			})
		}
		declared := context.metricLabels[selector.MetricName]
		if matcher.Label != "le" && len(declared) > 0 && !slices.Contains(declared, matcher.Label) {
			*violations = append(*violations, &ValidationError{
				Field: matcherField + ".label", Message: "matcher label is not declared by the metric plan",
			})
		}
	}
}

// sameSelectorDomain reports whether two selector lists cover the same
// metric with the same base matchers, ignoring extra status matchers on
// the left (numerator) side.
func sameSelectorDomain(left, right []*MetricSelector) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	normalize := func(selectors []*MetricSelector) []string {
		var domains []string
		for _, selector := range selectors {
			var base []LabelMatcher
			for _, matcher := range selector.Matchers {
				if matcher.Label != "status" {
					base = append(base, matcher)
				}
			}
			// Matcher order is not semantic; sort before rendering so the
			// domain comparison is order-independent.
			slices.SortFunc(base, func(first, second LabelMatcher) int {
				if first.Label != second.Label {
					if first.Label < second.Label {
						return -1
					}
					return 1
				}
				if first.Op != second.Op {
					if first.Op < second.Op {
						return -1
					}
					return 1
				}
				if first.Value != second.Value {
					if first.Value < second.Value {
						return -1
					}
					return 1
				}
				return 0
			})
			domains = append(domains, selector.MetricName+renderMatchers(base))
		}
		slices.Sort(domains)
		return domains
	}
	leftDomain, rightDomain := normalize(left), normalize(right)
	if len(leftDomain) != len(rightDomain) {
		return false
	}
	for index := range leftDomain {
		if leftDomain[index] != rightDomain[index] {
			return false
		}
	}
	return true
}

func renderMatchers(matchers []LabelMatcher) string {
	var builder strings.Builder
	for _, matcher := range matchers {
		builder.WriteString("{")
		builder.WriteString(matcher.Label)
		switch matcher.Op {
		case MatchEqual:
			builder.WriteString("=")
		case MatchNotEqual:
			builder.WriteString("!=")
		case MatchRegex:
			builder.WriteString("=~")
		}
		builder.WriteString(matcher.Value)
		builder.WriteString("}")
	}
	return builder.String()
}

func collectSelectors(expression Expression) []*MetricSelector {
	var selectors []*MetricSelector
	collect := func(node Expression) {
		switch current := node.(type) {
		case *MetricSelector:
			selectors = append(selectors, current)
		case *RateExpression:
			selectors = append(selectors, current.Selector)
		case *Aggregation:
			selectors = append(selectors, collectSelectors(current.Expr)...)
		case *HistogramQuantileExpression:
			selectors = append(selectors, collectSelectors(current.Expr)...)
		case *BinaryExpression:
			selectors = append(selectors, collectSelectors(current.Left)...)
			selectors = append(selectors, collectSelectors(current.Right)...)
		}
	}
	collect(expression)
	return selectors
}

func containsQuantile(quantiles []float64, wanted float64) bool {
	return slices.Contains(quantiles, wanted)
}

// ValidateTraceLink verifies a trace-link plan against its item: the
// datasource variable is the policy-fixed name, the service and operation
// are validated, the span name is bounded, and the plan carries no
// trace/request IDs, hosts or external URLs (its fields are structural).
func ValidateTraceLink(link *TraceLinkPlan, item *dashboard.DashboardItem, policy dashboard.DashboardPolicy) []*ValidationError {
	if link == nil {
		return []*ValidationError{{Field: "trace_link", Message: "link is nil"}}
	}
	if item == nil {
		return []*ValidationError{{Field: "trace_link", Message: "item is nil"}}
	}
	var violations []*ValidationError
	if link.DatasourceVariable != policy.DatasourceVariableName {
		violations = append(violations, &ValidationError{
			Field: "trace_link.datasource", Message: "datasource must be the policy-fixed variable",
		})
	}
	if !matcherValuePattern.MatchString(link.ServiceName) {
		violations = append(violations, &ValidationError{
			Field: "trace_link.service", Message: "service name is not a controlled value",
		})
	}
	if !operationValuePattern.MatchString(link.Operation) {
		violations = append(violations, &ValidationError{
			Field: "trace_link.operation", Message: "operation is not a controlled value",
		})
	}
	if !spanNamePattern.MatchString(link.SpanName) {
		violations = append(violations, &ValidationError{
			Field: "trace_link.span_name", Message: "span name is not a controlled value",
		})
	}
	spanPlanIDs := make(map[string]bool, len(item.Spans))
	for _, reference := range item.Spans {
		spanPlanIDs[reference.PlanID] = true
	}
	for _, planID := range link.PlanIDs {
		if !spanPlanIDs[planID] {
			violations = append(violations, &ValidationError{
				Field: "trace_link.plan_ids", Message: "references an undeclared span plan ID",
			})
		}
	}
	return violations
}

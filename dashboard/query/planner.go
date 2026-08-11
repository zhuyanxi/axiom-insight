package query

import (
	"fmt"
	"slices"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// PlanItemQueries builds every generatable query for one catalog item:
// rate, error ratio, fixed percentiles and in-flight, gated on the item
// capabilities. An unavailable capability produces a
// DASHBOARD_MISSING_REQUIRED_METRIC diagnostic and no substitute query
// (AC2). Values that fail validation are dropped with a
// DASHBOARD_SENSITIVE_VALUE_DROPPED diagnostic instead of entering an
// expression (AC3). Construction is O(Q) over the generated queries; the
// item's references arrive sorted by PlanID.
func PlanItemQueries(item *dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy) ([]QueryPlan, []TraceLinkPlan, []dashboard.Diagnostic) {
	if item == nil {
		return nil, nil, nil
	}
	serviceName, diagnostics := validatedServiceName(serviceName)
	for _, reference := range item.Metrics {
		if !metricNamePattern.MatchString(reference.Name) {
			diagnostics = append(diagnostics, dashboard.Diagnostic{
				Code: dashboard.CodeSensitiveValueDropped, TargetID: item.ID,
				Field: "metrics[].name", Message: "metric name is not a controlled machine name; the metric was dropped",
			})
		}
	}

	var plans []QueryPlan
	var traceLinks []TraceLinkPlan

	plans, diagnostics = appendRatePlan(plans, diagnostics, item, serviceName, policy)
	plans, diagnostics = appendErrorRatioPlan(plans, diagnostics, item, serviceName, policy)
	plans, diagnostics = appendPercentilePlans(plans, diagnostics, item, serviceName, policy)
	plans, diagnostics = appendInFlightPlan(plans, diagnostics, item, serviceName)
	traceLinks, diagnostics = appendTraceLink(traceLinks, diagnostics, item, serviceName, policy)

	sort.Slice(plans, func(left, right int) bool {
		return plans[left].CanonicalKey < plans[right].CanonicalKey
	})
	sort.Slice(traceLinks, func(left, right int) bool {
		return traceLinks[left].CanonicalKey < traceLinks[right].CanonicalKey
	})
	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].Code != diagnostics[right].Code {
			return diagnostics[left].Code < diagnostics[right].Code
		}
		if diagnostics[left].TargetID != diagnostics[right].TargetID {
			return diagnostics[left].TargetID < diagnostics[right].TargetID
		}
		return diagnostics[left].Field < diagnostics[right].Field
	})
	return plans, traceLinks, diagnostics
}

func appendRatePlan(plans []QueryPlan, diagnostics []dashboard.Diagnostic, item *dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy) ([]QueryPlan, []dashboard.Diagnostic) {
	if !item.Capabilities.Rate.Available {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "capabilities.rate", item.Capabilities.Rate.Reason))
	}
	counter, ok := firstMetric(item, "counter", "service", "operation")
	if !ok {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "metrics[].type", "no counter metric declares both service and operation attributes"))
	}
	selector := selectorFor(counter, serviceName, item.Operation, nil)
	expression := &Aggregation{
		By:   []string{"operation"},
		Expr: &RateExpression{Selector: selector, Interval: policy.RateInterval},
	}
	plan := QueryPlan{
		CanonicalKey: "query:" + string(QueryKindRate) + ":" + item.ID + ":rate",
		Kind:         QueryKindRate,
		ItemID:       item.ID,
		Purpose:      "rate",
		PlanIDs:      []string{counter.PlanID},
		Expression:   expression,
		Metadata: QueryMetadata{
			Kind: QueryKindRate, CanonicalKey: "query:" + string(QueryKindRate) + ":" + item.ID + ":rate",
			PlanIDs: []string{counter.PlanID}, Provenance: provenance(item, counter.PlanID),
			RateInterval: policy.RateInterval, HashVersion: dashboard.HashVersion,
		},
	}
	return append(plans, plan), diagnostics
}

func appendErrorRatioPlan(plans []QueryPlan, diagnostics []dashboard.Diagnostic, item *dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy) ([]QueryPlan, []dashboard.Diagnostic) {
	if !item.Capabilities.ErrorRatio.Available {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "capabilities.error_ratio", item.Capabilities.ErrorRatio.Reason))
	}
	counter, ok := firstMetric(item, "counter", "status")
	if !ok {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "metrics[].type", "no counter metric declares the status attribute"))
	}
	// AC5: both sides share the same selector domain; the numerator adds
	// exactly the fixed error-status matcher.
	domainMatchers := baseMatchers(counter, serviceName, item.Operation)
	numeratorSelector := selectorWith(counter.Name, append(domainMatchers,
		LabelMatcher{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern}))
	denominatorSelector := &MetricSelector{MetricName: counter.Name, Matchers: domainMatchers}
	by := []string{}
	if hasAttribute(counter, "operation") {
		by = []string{"operation"}
	}
	expression := &BinaryExpression{
		Op: BinaryDivide,
		Left: &Aggregation{
			By:   by,
			Expr: &RateExpression{Selector: numeratorSelector, Interval: policy.RateInterval},
		},
		Right: &Aggregation{
			By:   by,
			Expr: &RateExpression{Selector: denominatorSelector, Interval: policy.RateInterval},
		},
	}
	plan := QueryPlan{
		CanonicalKey: "query:" + string(QueryKindErrorRatio) + ":" + item.ID + ":error_ratio",
		Kind:         QueryKindErrorRatio,
		ItemID:       item.ID,
		Purpose:      "error_ratio",
		PlanIDs:      []string{counter.PlanID},
		Expression:   expression,
		Metadata: QueryMetadata{
			Kind: QueryKindErrorRatio, CanonicalKey: "query:" + string(QueryKindErrorRatio) + ":" + item.ID + ":error_ratio",
			PlanIDs: []string{counter.PlanID}, Provenance: provenance(item, counter.PlanID),
			RateInterval: policy.RateInterval, ErrorStatusPattern: ErrorStatusPattern,
			HashVersion: dashboard.HashVersion,
		},
	}
	return append(plans, plan), diagnostics
}

func appendPercentilePlans(plans []QueryPlan, diagnostics []dashboard.Diagnostic, item *dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy) ([]QueryPlan, []dashboard.Diagnostic) {
	if !item.Capabilities.Percentiles.Available {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "capabilities.percentiles", item.Capabilities.Percentiles.Reason))
	}
	histogram, ok := firstMetric(item, "histogram", "service", "operation")
	if !ok {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "metrics[].type", "no histogram metric declares both service and operation attributes"))
	}
	// The bucket selector always excludes the +Inf boundary and carries
	// the fixed le label plus the declared service/operation domain.
	bucketMatchers := append(baseMatchers(histogram, serviceName, item.Operation),
		LabelMatcher{Label: "le", Op: MatchNotEqual, Value: rateIntervalLeMatcher})
	for _, quantile := range FixedQuantiles {
		expression := &HistogramQuantileExpression{
			Quantile: quantile,
			Expr: &Aggregation{
				By:   []string{"le"},
				Expr: &RateExpression{Selector: selectorWith(histogram.Name, bucketMatchers), Interval: policy.RateInterval},
			},
		}
		purpose := purposeForQuantile(quantile)
		canonicalKey := "query:" + string(QueryKindPercentile) + ":" + item.ID + ":" + purpose
		plans = append(plans, QueryPlan{
			CanonicalKey: canonicalKey,
			Kind:         QueryKindPercentile,
			ItemID:       item.ID,
			Purpose:      purpose,
			PlanIDs:      []string{histogram.PlanID},
			Expression:   expression,
			Metadata: QueryMetadata{
				Kind: QueryKindPercentile, CanonicalKey: canonicalKey,
				PlanIDs: []string{histogram.PlanID}, Provenance: provenance(item, histogram.PlanID),
				RateInterval: policy.RateInterval, Quantiles: []float64{quantile},
				HashVersion: dashboard.HashVersion,
			},
		})
	}
	return plans, diagnostics
}

func appendInFlightPlan(plans []QueryPlan, diagnostics []dashboard.Diagnostic, item *dashboard.DashboardItem, serviceName string) ([]QueryPlan, []dashboard.Diagnostic) {
	if !item.Capabilities.InFlight.Available {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "capabilities.in_flight", item.Capabilities.InFlight.Reason))
	}
	gauge, ok := firstMetric(item, "gauge")
	if !ok {
		return plans, append(diagnostics,
			missingMetricDiagnostic(item, "metrics[].type", "no gauge metric is declared for this target"))
	}
	// In-flight is a gauge read: no rate(), no status matcher.
	selector := selectorFor(gauge, serviceName, item.Operation, nil)
	by := []string{}
	if hasAttribute(gauge, "operation") {
		by = []string{"operation"}
	}
	expression := &Aggregation{By: by, Expr: selector}
	canonicalKey := "query:" + string(QueryKindInFlight) + ":" + item.ID + ":in_flight"
	plan := QueryPlan{
		CanonicalKey: canonicalKey,
		Kind:         QueryKindInFlight,
		ItemID:       item.ID,
		Purpose:      "in_flight",
		PlanIDs:      []string{gauge.PlanID},
		Expression:   expression,
		Metadata: QueryMetadata{
			Kind: QueryKindInFlight, CanonicalKey: canonicalKey,
			PlanIDs: []string{gauge.PlanID}, Provenance: provenance(item, gauge.PlanID),
			RateInterval: "", HashVersion: dashboard.HashVersion,
		},
	}
	return append(plans, plan), diagnostics
}

func appendTraceLink(links []TraceLinkPlan, diagnostics []dashboard.Diagnostic, item *dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy) ([]TraceLinkPlan, []dashboard.Diagnostic) {
	if !policy.IncludeTraceLinks {
		return links, diagnostics
	}
	if !item.Capabilities.TraceLink.Available || len(item.Spans) == 0 {
		reason := item.Capabilities.TraceLink.Reason
		if len(item.Spans) == 0 {
			reason = "no span plan is declared for this target"
		}
		return links, append(diagnostics,
			missingMetricDiagnostic(item, "capabilities.trace_link", reason))
	}
	span := item.Spans[0] // sorted by PlanID
	if !spanNamePattern.MatchString(span.Name) {
		return links, append(diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeSensitiveValueDropped, TargetID: item.ID,
			Field: "spans[].name", Message: "span name is not a controlled value; the trace link was dropped",
		})
	}
	operation := item.Operation
	if !operationValuePattern.MatchString(operation) {
		operation = ""
	}
	planIDs := make([]string, 0, len(item.Spans))
	for _, reference := range item.Spans {
		planIDs = append(planIDs, reference.PlanID)
	}
	sort.Strings(planIDs)
	canonicalKey := "trace:" + item.ID
	link := TraceLinkPlan{
		CanonicalKey:       canonicalKey,
		ItemID:             item.ID,
		PlanIDs:            planIDs,
		DatasourceVariable: policy.DatasourceVariableName,
		ServiceName:        serviceName,
		Operation:          operation,
		SpanName:           span.Name,
		Metadata: QueryMetadata{
			Kind: "trace_link", CanonicalKey: canonicalKey,
			PlanIDs: planIDs, Provenance: provenance(item, ""),
			HashVersion: dashboard.HashVersion,
		},
	}
	return append(links, link), diagnostics
}

// PlanOperationBreakdown builds one rate query per controlled operation
// value (P2-05 task 6). The result count is the number of operation
// values; above MaxBreakdownOperations the build fails with
// DASHBOARD_PANEL_LIMIT_EXCEEDED instead of truncating. Invalid values
// are dropped with diagnostics, never embedded.
func PlanOperationBreakdown(metric dashboard.SignalReference, operations []string, serviceName string, policy dashboard.DashboardPolicy) ([]QueryPlan, []dashboard.Diagnostic, error) {
	if len(operations) > MaxBreakdownOperations {
		return nil, nil, &dashboard.CatalogError{
			Code: dashboard.CodePanelLimitExceeded, Field: "operation_breakdown",
			Message: fmt.Sprintf("operation breakdown exceeds the fixed limit of %d operation values", MaxBreakdownOperations),
		}
	}
	serviceName, diagnostics := validatedServiceName(serviceName)
	var plans []QueryPlan
	for _, operation := range operations {
		if !operationValuePattern.MatchString(operation) {
			diagnostics = append(diagnostics, dashboard.Diagnostic{
				Code: dashboard.CodeSensitiveValueDropped, TargetID: metric.PlanID,
				Field: "operation_breakdown[].operation", Message: "operation value is not a controlled value; it was dropped",
			})
			continue
		}
		matchers := baseMatchers(metric, serviceName, operation)
		expression := &Aggregation{
			By:   []string{"operation"},
			Expr: &RateExpression{Selector: &MetricSelector{MetricName: metric.Name, Matchers: matchers}, Interval: policy.RateInterval},
		}
		canonicalKey := "query:" + string(QueryKindBreakdown) + ":" + metric.PlanID + ":" + operation
		plans = append(plans, QueryPlan{
			CanonicalKey: canonicalKey,
			Kind:         QueryKindBreakdown,
			ItemID:       metric.PlanID,
			Purpose:      "breakdown",
			PlanIDs:      []string{metric.PlanID},
			Expression:   expression,
			Metadata: QueryMetadata{
				Kind: QueryKindBreakdown, CanonicalKey: canonicalKey,
				PlanIDs: []string{metric.PlanID}, Provenance: []string{"operation_breakdown[" + metric.PlanID + "]"},
				RateInterval: policy.RateInterval, OperationValues: []string{operation},
				HashVersion: dashboard.HashVersion,
			},
		})
	}
	sort.Slice(plans, func(left, right int) bool { return plans[left].CanonicalKey < plans[right].CanonicalKey })
	return plans, diagnostics, nil
}

// selectorFor builds the controlled selector for one reference: the exact
// declared metric name plus matchers for the declared vocabulary labels
// (service and operation) with validated values.
func selectorFor(reference dashboard.SignalReference, serviceName, operation string, extra []LabelMatcher) *MetricSelector {
	return selectorWith(reference.Name, append(baseMatchers(reference, serviceName, operation), extra...))
}

func selectorWith(metricName string, matchers []LabelMatcher) *MetricSelector {
	sort.Slice(matchers, func(left, right int) bool { return matchers[left].Label < matchers[right].Label })
	return &MetricSelector{MetricName: metricName, Matchers: matchers}
}

// baseMatchers adds service and operation matchers only when the metric
// plan declares the attribute and the value passes the controlled-value
// gate. Unvalidated values never become matcher content.
func baseMatchers(reference dashboard.SignalReference, serviceName, operation string) []LabelMatcher {
	var matchers []LabelMatcher
	if hasAttribute(reference, "service") && serviceName != "" {
		matchers = append(matchers, LabelMatcher{Label: "service", Op: MatchEqual, Value: serviceName})
	}
	if hasAttribute(reference, "operation") && operation != "" && operationValuePattern.MatchString(operation) {
		matchers = append(matchers, LabelMatcher{Label: "operation", Op: MatchEqual, Value: operation})
	}
	return matchers
}

func hasAttribute(reference dashboard.SignalReference, wanted string) bool {
	return slices.Contains(reference.Attributes, wanted)
}

// firstMetric returns the first reference of the wanted type in
// PlanID-sorted order, optionally requiring the declared attributes.
// References whose names fail the controlled machine-name gate are
// skipped; their queries are reported by the planner instead.
func firstMetric(item *dashboard.DashboardItem, metricType string, requireAttributes ...string) (dashboard.SignalReference, bool) {
	for _, reference := range item.Metrics {
		if reference.Type != metricType || !metricNamePattern.MatchString(reference.Name) {
			continue
		}
		missing := false
		for _, attribute := range requireAttributes {
			if !hasAttribute(reference, attribute) {
				missing = true
				break
			}
		}
		if missing {
			continue
		}
		return reference, true
	}
	return dashboard.SignalReference{}, false
}

func validatedServiceName(serviceName string) (string, []dashboard.Diagnostic) {
	if matcherValuePattern.MatchString(serviceName) {
		return serviceName, nil
	}
	return "", []dashboard.Diagnostic{{
		Code: dashboard.CodeSensitiveValueDropped, TargetID: "service",
		Field: "service.name", Message: "service name is not a controlled value; service matchers were dropped",
	}}
}

func missingMetricDiagnostic(item *dashboard.DashboardItem, field, message string) dashboard.Diagnostic {
	return dashboard.Diagnostic{
		Code: dashboard.CodeMissingRequiredMetric, TargetID: item.ID, Field: field, Message: message,
	}
}

func provenance(item *dashboard.DashboardItem, planID string) []string {
	source := append([]string(nil), item.Provenance...)
	if planID != "" {
		source = append(source, "metrics["+planID+"]")
	}
	return source
}

func purposeForQuantile(quantile float64) string {
	switch quantile {
	case 0.50:
		return "p50"
	case 0.95:
		return "p95"
	default:
		return "p99"
	}
}

package overview

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

// panelBuilder assembles the fixed P2-06 panel set from the catalog's
// metric families. Panel purposes, widths and units are pinned; a family
// capability gap omits the panel with DASHBOARD_MISSING_REQUIRED_METRIC.
type panelBuilder struct {
	serviceName string
	policy      dashboard.DashboardPolicy
	families    []*metricFamily
	hasItems    bool
	diagnostics *[]dashboard.Diagnostic
}

type panelSpec struct {
	Purpose     string
	Title       string
	Description string
	Type        string
	Width       int
	Height      int
	Unit        string
	NoValue     string
	Targets     []Target
}

func (builder *panelBuilder) panels() ([]Panel, error) {
	if !builder.hasItems {
		return nil, nil
	}
	specs, err := builder.specs()
	if err != nil {
		return nil, err
	}
	total := 0
	for _, spec := range specs {
		total += len(spec.Targets)
	}
	if total > MaxOverviewQueries {
		return nil, queryLimitError(total)
	}

	panels := make([]Panel, 0, len(specs))
	for _, spec := range specs {
		if len(spec.Targets) == 0 {
			*builder.diagnostics = append(*builder.diagnostics, missingPanelDiagnostic(spec.Purpose))
			continue
		}
		panels = append(panels, Panel{
			Key:     dashboard.PanelIDKey(dashboard.CategoryServiceOverview, PanelKeyItemID, spec.Purpose),
			Purpose: spec.Purpose, Title: spec.Title, Description: spec.Description,
			Type: spec.Type, Width: spec.Width, Height: spec.Height,
			Unit: spec.Unit, NoValue: spec.NoValue, Targets: spec.Targets,
		})
	}
	return panels, nil
}

func (builder *panelBuilder) specs() ([]panelSpec, error) {
	rateTargets, err := builder.familyTargets(func(capabilities familyCapabilities) bool {
		return capabilities.Rate
	}, rateQuery)
	if err != nil {
		return nil, err
	}
	errorTargets, err := builder.familyTargets(func(capabilities familyCapabilities) bool {
		return capabilities.ErrorRatio
	}, errorRatioQuery)
	if err != nil {
		return nil, err
	}
	p50Targets, err := builder.percentileTargets(0.50)
	if err != nil {
		return nil, err
	}
	p95Targets, err := builder.percentileTargets(0.95)
	if err != nil {
		return nil, err
	}
	p99Targets, err := builder.percentileTargets(0.99)
	if err != nil {
		return nil, err
	}
	inFlightTargets, err := builder.familyTargets(func(capabilities familyCapabilities) bool {
		return capabilities.InFlight
	}, inFlightQuery)
	if err != nil {
		return nil, err
	}
	topFailingTargets, err := builder.familyTargets(func(capabilities familyCapabilities) bool {
		return capabilities.ErrorRatio
	}, topFailingQuery)
	if err != nil {
		return nil, err
	}

	return []panelSpec{
		{
			Purpose: "requests_rate", Title: "Request Rate",
			Description: "Rate of handled operations per second across all dashboard categories sharing the same metric family.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "ops/s", NoValue: "0", Targets: rateTargets,
		},
		{
			Purpose: "error_ratio", Title: "Error Ratio",
			Description: "Share of operations ending in the fixed error status pattern (5xx or error) across all metric families.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "percent", NoValue: "0", Targets: errorTargets,
		},
		{
			Purpose: "p50", Title: "p50 Duration",
			Description: "Median operation duration in seconds (histogram_quantile 0.50) across all metric families.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "s", NoValue: "0", Targets: p50Targets,
		},
		{
			Purpose: "p95", Title: "p95 Duration",
			Description: "95th percentile operation duration in seconds across all metric families.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "s", NoValue: "0", Targets: p95Targets,
		},
		{
			Purpose: "p99", Title: "p99 Duration",
			Description: "99th percentile operation duration in seconds across all metric families.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "s", NoValue: "0", Targets: p99Targets,
		},
		{
			Purpose: "in_flight", Title: "In-Flight Operations",
			Description: "Number of operations currently in flight across all gauge families.",
			Type:        model.PanelTypeStat, Width: dashboard.PanelWidthStat, Height: 8,
			Unit: "short", NoValue: "0", Targets: inFlightTargets,
		},
		{
			Purpose: "top_failing", Title: "Top Failing Operations",
			Description: "Error rate per operation across all metric families; the table is client-sorted to surface the highest failure rate.",
			Type:        model.PanelTypeTable, Width: dashboard.PanelWidthTable, Height: 8,
			Unit: "ops/s", NoValue: "0", Targets: topFailingTargets,
		},
	}, nil
}

// familyTargets builds one target per family that passes the capability
// gate and validates it against every aggregated item (P2-05 AC1).
func (builder *panelBuilder) familyTargets(
	gate func(familyCapabilities) bool,
	build func(*metricFamily, string, dashboard.DashboardPolicy) (*query.QueryPlan, string),
) ([]Target, error) {
	var targets []Target
	for _, family := range builder.families {
		if !gate(family.Capabilities) {
			continue
		}
		plan, legend := build(family, builder.serviceName, builder.policy)
		target, err := builder.validatedTarget(plan, family, legend)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (builder *panelBuilder) percentileTargets(quantile float64) ([]Target, error) {
	var targets []Target
	for _, family := range builder.families {
		if !family.Capabilities.Percentiles {
			continue
		}
		plan := percentileQuery(family, builder.serviceName, builder.policy, quantile)
		target, err := builder.validatedTarget(plan, family, family.Name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (builder *panelBuilder) validatedTarget(plan *query.QueryPlan, family *metricFamily, legend string) (Target, error) {
	if violations := query.ValidateOverviewPlan(plan, family.Items, builder.serviceName); len(violations) > 0 {
		return Target{}, &dashboard.CatalogError{
			Code:    dashboard.CodeRenderError,
			Field:   "overview.queries[" + plan.CanonicalKey + "]",
			Message: fmt.Sprintf("overview query failed P2-05 validation: %v", violations[0]),
		}
	}
	expression, err := query.Render(plan.Expression)
	if err != nil {
		return Target{}, &dashboard.CatalogError{
			Code:    dashboard.CodeRenderError,
			Field:   "overview.queries[" + plan.CanonicalKey + "]",
			Message: err.Error(),
		}
	}
	return Target{
		CanonicalKey: plan.CanonicalKey, Kind: string(plan.Kind),
		Categories: append([]dashboard.Category(nil), family.Categories...),
		ItemIDs:    append([]string(nil), family.ItemIDs...),
		PlanIDs:    append([]string(nil), family.PlanIDs...),
		Expr:       expression, LegendFormat: legend, Query: plan,
	}, nil
}

func missingPanelDiagnostic(purpose string) dashboard.Diagnostic {
	return dashboard.Diagnostic{
		Code: dashboard.CodeMissingRequiredMetric, TargetID: PanelKeyItemID,
		Field:   "overview.panels." + purpose,
		Message: "no metric family supports this overview panel; the panel is omitted",
	}
}

func rateQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
	expression := &query.Aggregation{
		By:   []string{"operation"},
		Expr: &query.RateExpression{Selector: selectorForFamily(family, serviceName, nil), Interval: policy.RateInterval},
	}
	return overviewPlan(query.QueryKindRate, family, "rate", expression, policy), "{{operation}}"
}

func errorRatioQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
	numerator := selectorForFamily(family, serviceName, []query.LabelMatcher{{
		Label: "status", Op: query.MatchRegex, Value: query.ErrorStatusPattern,
	}})
	denominator := selectorForFamily(family, serviceName, nil)
	left := &query.Aggregation{
		By:   []string{"operation"},
		Expr: &query.RateExpression{Selector: numerator, Interval: policy.RateInterval},
	}
	right := &query.Aggregation{
		By:   []string{"operation"},
		Expr: &query.RateExpression{Selector: denominator, Interval: policy.RateInterval},
	}
	expression := &query.BinaryExpression{Op: query.BinaryDivide, Left: left, Right: right}
	return overviewPlan(query.QueryKindErrorRatio, family, "error_ratio", expression, policy), "{{operation}}"
}

func percentileQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy, quantile float64) *query.QueryPlan {
	selector := selectorForFamily(family, serviceName, []query.LabelMatcher{{
		Label: "le", Op: query.MatchNotEqual, Value: "+Inf",
	}})
	expression := &query.HistogramQuantileExpression{
		Quantile: quantile,
		Expr: &query.Aggregation{
			By:   []string{"le"},
			Expr: &query.RateExpression{Selector: selector, Interval: policy.RateInterval},
		},
	}
	return overviewPlan(query.QueryKindPercentile, family, purposeForQuantile(quantile), expression, policy)
}

func inFlightQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
	var by []string
	legend := family.Name
	if hasAttribute(family, "operation") {
		by = []string{"operation"}
		legend = "{{operation}}"
	}
	expression := &query.Aggregation{By: by, Expr: selectorForFamily(family, serviceName, nil)}
	return overviewPlan(query.QueryKindInFlight, family, "in_flight", expression, policy), legend
}

func topFailingQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
	selector := selectorForFamily(family, serviceName, []query.LabelMatcher{{
		Label: "status", Op: query.MatchRegex, Value: query.ErrorStatusPattern,
	}})
	expression := &query.Aggregation{
		By:   []string{"operation"},
		Expr: &query.RateExpression{Selector: selector, Interval: policy.RateInterval},
	}
	return overviewPlan(query.QueryKindTopFailing, family, "top_failing", expression, policy), "{{operation}}"
}

// selectorForFamily builds the controlled selector for one family: the
// exact declared metric name plus the service matcher when the family
// declares the service label and the service value is validated.
func selectorForFamily(family *metricFamily, serviceName string, extra []query.LabelMatcher) *query.MetricSelector {
	var matchers []query.LabelMatcher
	if serviceName != "" && hasAttribute(family, "service") {
		matchers = append(matchers, query.LabelMatcher{
			Label: "service", Op: query.MatchEqual, Value: serviceName,
		})
	}
	matchers = append(matchers, extra...)
	sort.Slice(matchers, func(left, right int) bool {
		if matchers[left].Label != matchers[right].Label {
			return matchers[left].Label < matchers[right].Label
		}
		if matchers[left].Op != matchers[right].Op {
			return matchers[left].Op < matchers[right].Op
		}
		return matchers[left].Value < matchers[right].Value
	})
	return &query.MetricSelector{MetricName: family.Name, Matchers: matchers}
}

func overviewPlan(kind query.QueryKind, family *metricFamily, purpose string, expression query.Expression, policy dashboard.DashboardPolicy) *query.QueryPlan {
	canonicalKey := "query:" + string(kind) + ":overview:" + family.Key
	metadata := query.QueryMetadata{
		Kind: kind, CanonicalKey: canonicalKey,
		PlanIDs:      append([]string(nil), family.PlanIDs...),
		Provenance:   []string{"overview:families[" + family.Key + "]"},
		RateInterval: policy.RateInterval,
		HashVersion:  dashboard.HashVersion,
	}
	if kind == query.QueryKindPercentile {
		metadata.Quantiles = []float64{quantileForPurpose(purpose)}
	}
	if kind == query.QueryKindErrorRatio || kind == query.QueryKindTopFailing {
		metadata.ErrorStatusPattern = query.ErrorStatusPattern
	}
	if kind == query.QueryKindInFlight {
		metadata.RateInterval = ""
	}
	return &query.QueryPlan{
		CanonicalKey: canonicalKey, Kind: kind, ItemID: PanelKeyItemID,
		Purpose:    purpose,
		PlanIDs:    append([]string(nil), family.PlanIDs...),
		Expression: expression, Metadata: metadata,
	}
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

func quantileForPurpose(purpose string) float64 {
	switch purpose {
	case "p50":
		return 0.50
	case "p95":
		return 0.95
	default:
		return 0.99
	}
}

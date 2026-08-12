package category

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

// metricFamily is one Kafka metric semantic family: items whose plans
// declare the same controlled name, type and label schema.
type metricFamily struct {
	Key          string
	Name         string
	Type         string
	Unit         string
	Attributes   []string
	PlanIDs      []string
	ItemIDs      []string
	Categories   []dashboard.Category
	Items        []dashboard.DashboardItem
	Capabilities familyCapabilities
}

type familyCapabilities struct {
	Rate        bool
	ErrorRatio  bool
	Percentiles bool
}

func familyCapabilitiesFrom(metricType string, attributes []string) familyCapabilities {
	has := func(label string) bool {
		for _, attribute := range attributes {
			if attribute == label {
				return true
			}
		}
		return false
	}
	return familyCapabilities{
		Rate:        metricType == "counter" && has("service") && has("operation"),
		ErrorRatio:  metricType == "counter" && has("service") && has("status"),
		Percentiles: metricType == "histogram" && has("service"),
	}
}

func hasAttribute(family *metricFamily, label string) bool {
	for _, attribute := range family.Attributes {
		if attribute == label {
			return true
		}
	}
	return false
}

func appendUniqueItem(values []dashboard.DashboardItem, wanted dashboard.DashboardItem) []dashboard.DashboardItem {
	for _, item := range values {
		if item.ID == wanted.ID {
			return values
		}
	}
	return append(values, wanted)
}

func appendUniqueString(values []string, wanted string) []string {
	for _, value := range values {
		if value == wanted {
			return values
		}
	}
	return append(values, wanted)
}

func appendUniqueCategory(values []dashboard.Category, wanted dashboard.Category) []dashboard.Category {
	for _, value := range values {
		if value == wanted {
			return values
		}
	}
	return append(values, wanted)
}

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

// kafkaClass separates the two Kafka operation classes inside one row.
type kafkaClass struct {
	dependencyKind string
	title          string
}

var kafkaClasses = []kafkaClass{
	{dependencyKind: "kafka_producer", title: "producer"},
	{dependencyKind: "kafka_consumer", title: "consumer"},
}

// BuildKafka plans the Kafka row (P2-08). Only items built from
// KAFKA_PRODUCER/KAFKA_CONSUMER dependency plans enter; Dependency.value,
// topics, groups and payloads never become labels, titles, legends or
// link parameters. Producer and consumer operation classes get distinct
// controlled subtitles inside the same row.
func BuildKafka(catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) (*Plan, error) {
	if catalog == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "catalog", Message: "catalog is nil",
		}
	}
	serviceName, diagnostics := validateServiceName(catalog.ServiceName)
	// Catalog-level security diagnostics (unsupported or policy-blocked
	// targets) are preserved so AC3 keeps DASHBOARD_UNSUPPORTED_TARGET.
	diagnostics = append(diagnostics, catalog.Diagnostics...)

	items := kafkaItems(catalog.Items)
	row := RowPlan{
		Category:    dashboard.CategoryKafka,
		Title:       dashboard.CategoryTitle(dashboard.CategoryKafka),
		Description: "Kafka producer and consumer calls identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
		RowKey:      dashboard.RowIDKey(dashboard.CategoryKafka, RowPurpose),
	}
	if len(items) == 0 {
		diagnostics = append(diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(dashboard.CategoryKafka),
			Field:   "rows.kafka",
			Message: "no Kafka producer or consumer entities; the row is omitted",
		})
		sortDiagnostics(diagnostics)
		return &Plan{Rows: []RowPlan{row}, Diagnostics: diagnostics}, nil
	}

	for _, class := range kafkaClasses {
		classItems := classItems(items, class.dependencyKind)
		if len(classItems) == 0 {
			continue
		}
		classPanels, classTargets, err := kafkaClassPanels(class, classItems, serviceName, policy, &diagnostics)
		if err != nil {
			return nil, err
		}
		row.Panels = append(row.Panels, classPanels...)
		if classTargets > MaxQueriesPerCategory {
			return nil, limitError(dashboard.CategoryKafka, "queries", classTargets, MaxQueriesPerCategory)
		}
	}
	sort.Slice(row.Panels, func(left, right int) bool { return row.Panels[left].Key < row.Panels[right].Key })
	if len(row.Panels) > MaxPanelsPerCategory {
		return nil, limitError(dashboard.CategoryKafka, "panels", len(row.Panels), MaxPanelsPerCategory)
	}
	totalQueries := 0
	for _, panel := range row.Panels {
		totalQueries += len(panel.Targets)
	}
	if totalQueries > MaxQueriesPerCategory {
		return nil, limitError(dashboard.CategoryKafka, "queries", totalQueries, MaxQueriesPerCategory)
	}
	if len(row.Panels) == 0 {
		diagnostics = append(diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(dashboard.CategoryKafka),
			Field:   "rows.kafka",
			Message: "no generatable Kafka panels; the row is omitted",
		})
	}
	if diagnostics == nil {
		diagnostics = []dashboard.Diagnostic{}
	}
	sortDiagnostics(diagnostics)
	return &Plan{Rows: []RowPlan{row}, Diagnostics: diagnostics}, nil
}

func kafkaItems(items []dashboard.DashboardItem) []dashboard.DashboardItem {
	var selected []dashboard.DashboardItem
	for _, item := range items {
		if item.Category == dashboard.CategoryKafka && item.Target.Kind == "dependency" {
			selected = append(selected, item)
		}
	}
	return selected
}

func classItems(items []dashboard.DashboardItem, dependencyKind string) []dashboard.DashboardItem {
	var selected []dashboard.DashboardItem
	for _, item := range items {
		if item.DependencyKind == dependencyKind {
			selected = append(selected, item)
		}
	}
	return selected
}

func kafkaClassPanels(class kafkaClass, items []dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) ([]Panel, int, error) {
	families := kafkaFamilies(items, class.title, diagnostics)
	links := kafkaTraceLinks(items, class, serviceName, policy)

	specs := []struct {
		purpose string
		targets func() ([]Target, error)
	}{
		{purpose: "rate", targets: func() ([]Target, error) {
			return kafkaFamilyTargets(families, func(capabilities familyCapabilities) bool { return capabilities.Rate },
				kafkaRateQuery, serviceName, policy)
		}},
		{purpose: "error_ratio", targets: func() ([]Target, error) {
			return kafkaFamilyTargets(families, func(capabilities familyCapabilities) bool { return capabilities.ErrorRatio },
				kafkaErrorRatioQuery, serviceName, policy)
		}},
		{purpose: "p50", targets: func() ([]Target, error) {
			return kafkaPercentileTargets(families, 0.50, serviceName, policy)
		}},
		{purpose: "p95", targets: func() ([]Target, error) {
			return kafkaPercentileTargets(families, 0.95, serviceName, policy)
		}},
		{purpose: "p99", targets: func() ([]Target, error) {
			return kafkaPercentileTargets(families, 0.99, serviceName, policy)
		}},
	}
	var panels []Panel
	total := 0
	for _, spec := range specs {
		targets, err := spec.targets()
		if err != nil {
			return nil, 0, err
		}
		if len(targets) == 0 {
			*diagnostics = append(*diagnostics, missingKafkaPanelDiagnostic(class, spec.purpose))
			continue
		}
		panel := kafkaPanel(class, spec.purpose, targets)
		panel.Links = links
		panels = append(panels, panel)
		total += len(targets)
	}

	table, err := kafkaOperationTable(items, class, serviceName, policy, diagnostics)
	if err != nil {
		return nil, 0, err
	}
	if len(table.Targets) > 0 {
		table.Links = links
		panels = append(panels, table)
		total += len(table.Targets)
	}
	return panels, total, nil
}

func kafkaPanel(class kafkaClass, purpose string, targets []Target) Panel {
	spec := kafkaSpecForPurpose(class, purpose)
	return Panel{
		Key:         dashboard.PanelIDKey(dashboard.CategoryKafka, class.title, purpose),
		ItemID:      class.title,
		Purpose:     purpose,
		Title:       dashboard.PanelTitle(dashboard.CategoryKafka, class.title, spec.title),
		Description: spec.description,
		Type:        spec.panelType,
		Width:       spec.width,
		Height:      spec.height,
		Unit:        spec.unit,
		NoValue:     spec.noValue,
		Targets:     targets,
	}
}

func kafkaSpecForPurpose(class kafkaClass, purpose string) panelSpec {
	switch purpose {
	case "rate":
		return panelSpec{
			title:       "request rate",
			description: fmt.Sprintf("Rate of Kafka %s operations; requires runtime instrumentation from the Phase 1 Instrumentation Plan.", class.title),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	case "error_ratio":
		return panelSpec{
			title:       "error ratio",
			description: fmt.Sprintf("Share of Kafka %s operations ending in the fixed error status pattern; requires runtime instrumentation.", class.title),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "percent", noValue: "0", legend: "{{operation}}",
		}
	case "p50":
		return panelSpec{
			title:       "p50 duration",
			description: fmt.Sprintf("Median Kafka %s operation duration in seconds; requires runtime instrumentation.", class.title),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p95":
		return panelSpec{
			title:       "p95 duration",
			description: fmt.Sprintf("95th percentile Kafka %s operation duration in seconds; requires runtime instrumentation.", class.title),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p99":
		return panelSpec{
			title:       "p99 duration",
			description: fmt.Sprintf("99th percentile Kafka %s operation duration in seconds; requires runtime instrumentation.", class.title),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	default:
		return panelSpec{
			title:       "operations",
			description: fmt.Sprintf("Operation rate breakdown across the Kafka %s counter metric families; requires runtime instrumentation.", class.title),
			panelType:   model.PanelTypeTable, width: dashboard.PanelWidthTable, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	}
}

func missingKafkaPanelDiagnostic(class kafkaClass, purpose string) dashboard.Diagnostic {
	return dashboard.Diagnostic{
		Code: dashboard.CodeMissingRequiredMetric, TargetID: "kafka:" + class.title,
		Field:   "rows.kafka." + class.title + "." + purpose,
		Message: "no Kafka " + class.title + " metric family supports this panel; the panel is omitted",
	}
}

func kafkaFamilyTargets(
	families []metricFamily,
	gate func(familyCapabilities) bool,
	build func(*metricFamily, string, dashboard.DashboardPolicy) (*query.QueryPlan, string),
	serviceName string,
	policy dashboard.DashboardPolicy,
) ([]Target, error) {
	var targets []Target
	for _, family := range families {
		if !gate(family.Capabilities) {
			continue
		}
		plan, legend := build(&family, serviceName, policy)
		if violations := query.ValidateOverviewPlan(plan, family.Items, serviceName); len(violations) > 0 {
			return nil, &dashboard.CatalogError{
				Code:    dashboard.CodeRenderError,
				Field:   "queries[" + plan.CanonicalKey + "]",
				Message: fmt.Sprintf("Kafka query failed P2-05 validation: %v", violations[0]),
			}
		}
		expression, err := query.Render(plan.Expression)
		if err != nil {
			return nil, &dashboard.CatalogError{
				Code:    dashboard.CodeRenderError,
				Field:   "queries[" + plan.CanonicalKey + "]",
				Message: err.Error(),
			}
		}
		planID := ""
		if len(plan.PlanIDs) > 0 {
			planID = plan.PlanIDs[0]
		}
		targets = append(targets, Target{
			CanonicalKey: plan.CanonicalKey,
			Kind:         string(plan.Kind),
			PlanID:       planID,
			TargetID:     family.ItemIDs[0],
			Expr:         expression,
			LegendFormat: legend,
		})
	}
	return targets, nil
}

func kafkaPercentileTargets(families []metricFamily, quantile float64, serviceName string, policy dashboard.DashboardPolicy) ([]Target, error) {
	var targets []Target
	for _, family := range families {
		if !family.Capabilities.Percentiles {
			continue
		}
		plan := kafkaPercentileQuery(&family, serviceName, policy, quantile)
		if violations := query.ValidateOverviewPlan(plan, family.Items, serviceName); len(violations) > 0 {
			return nil, &dashboard.CatalogError{
				Code:    dashboard.CodeRenderError,
				Field:   "queries[" + plan.CanonicalKey + "]",
				Message: fmt.Sprintf("Kafka query failed P2-05 validation: %v", violations[0]),
			}
		}
		expression, err := query.Render(plan.Expression)
		if err != nil {
			return nil, &dashboard.CatalogError{
				Code:    dashboard.CodeRenderError,
				Field:   "queries[" + plan.CanonicalKey + "]",
				Message: err.Error(),
			}
		}
		planID := ""
		if len(plan.PlanIDs) > 0 {
			planID = plan.PlanIDs[0]
		}
		targets = append(targets, Target{
			CanonicalKey: plan.CanonicalKey,
			Kind:         string(plan.Kind),
			PlanID:       planID,
			TargetID:     family.ItemIDs[0],
			Expr:         expression,
			LegendFormat: family.Name,
		})
	}
	return targets, nil
}

func kafkaTraceLinks(items []dashboard.DashboardItem, class kafkaClass, serviceName string, policy dashboard.DashboardPolicy) []model.Link {
	if !policy.IncludeTraceLinks {
		return nil
	}
	var links []model.Link
	for _, item := range items {
		if !item.Capabilities.TraceLink.Available || len(item.Spans) == 0 {
			continue
		}
		span := item.Spans[0]
		if !query.ValidSpanName(span.Name) {
			continue
		}
		operation := item.Operation
		if !query.ValidOperationValue(operation) {
			operation = ""
		}
		links = append(links, model.Link{
			Title: "Traces",
			URL: traceLinkURL(query.TraceLinkPlan{
				DatasourceVariable: policy.DatasourceVariableName,
				ServiceName:        serviceName,
				Operation:          operation,
				SpanName:           span.Name,
			}),
			TargetBlank: true,
		})
	}
	sort.Slice(links, func(left, right int) bool { return links[left].URL < links[right].URL })
	return links
}

func kafkaOperationTable(items []dashboard.DashboardItem, class kafkaClass, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) (Panel, error) {
	targets, err := operationTableTargets(items, serviceName, policy, diagnostics)
	if err != nil {
		return Panel{}, err
	}
	if len(targets) > model.MaxTargetsPerPanel {
		return Panel{}, &dashboard.CatalogError{
			Code:    dashboard.CodePanelLimitExceeded,
			Field:   "rows.kafka." + class.title + ".operations.targets",
			Message: fmt.Sprintf("Kafka %s operation table exceeds the fixed limit of %d targets", class.title, model.MaxTargetsPerPanel),
		}
	}
	panel := kafkaPanel(class, "operations", targets)
	return panel, nil
}

// kafkaFamilies groups one operation class's metric references into
// semantic families with overview-style capabilities. Topic, group,
// payload and other raw dependency values never enter a key.
func kafkaFamilies(items []dashboard.DashboardItem, class string, diagnostics *[]dashboard.Diagnostic) []metricFamily {
	byKey := make(map[string]*metricFamily)
	for _, item := range items {
		for _, metric := range item.Metrics {
			if !query.ValidMetricName(metric.Name) {
				*diagnostics = append(*diagnostics, dashboard.Diagnostic{
					Code: dashboard.CodeSensitiveValueDropped, TargetID: item.ID,
					Field: "metrics[].name", Message: "metric name is not a controlled machine name; the metric was dropped",
				})
				continue
			}
			attributes := append([]string(nil), metric.Attributes...)
			sort.Strings(attributes)
			key := metric.Name + "\x00" + metric.Type + "\x00" + slicesConcat(attributes)
			family := byKey[key]
			if family == nil {
				family = &metricFamily{
					Key: key, Name: metric.Name, Type: metric.Type, Unit: metric.Unit,
					Attributes:   attributes,
					Capabilities: familyCapabilitiesFrom(metric.Type, attributes),
				}
				byKey[key] = family
			}
			family.PlanIDs = appendUniqueString(family.PlanIDs, metric.PlanID)
			family.ItemIDs = appendUniqueString(family.ItemIDs, item.ID)
			family.Categories = appendUniqueCategory(family.Categories, item.Category)
			family.Items = appendUniqueItem(family.Items, item)
		}
	}
	families := make([]metricFamily, 0, len(byKey))
	for _, family := range byKey {
		sort.Strings(family.PlanIDs)
		sort.Strings(family.ItemIDs)
		sort.Slice(family.Categories, func(left, right int) bool {
			return family.Categories[left] < family.Categories[right]
		})
		sort.Slice(family.Items, func(left, right int) bool {
			return family.Items[left].ID < family.Items[right].ID
		})
		families = append(families, *family)
	}
	sort.Slice(families, func(left, right int) bool { return families[left].Key < families[right].Key })
	return families
}

func slicesConcat(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}

func kafkaRateQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
	expression := &query.Aggregation{
		By:   []string{"operation"},
		Expr: &query.RateExpression{Selector: selectorForFamily(family, serviceName, nil), Interval: policy.RateInterval},
	}
	return kafkaOverviewPlan(family, "rate", expression, policy), "{{operation}}"
}

func kafkaErrorRatioQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy) (*query.QueryPlan, string) {
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
	return kafkaOverviewPlan(family, "error_ratio", &query.BinaryExpression{Op: query.BinaryDivide, Left: left, Right: right}, policy), "{{operation}}"
}

func kafkaPercentileQuery(family *metricFamily, serviceName string, policy dashboard.DashboardPolicy, quantile float64) *query.QueryPlan {
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
	return kafkaOverviewPlan(family, purposeForQuantile(quantile), expression, policy)
}

func kafkaOverviewPlan(family *metricFamily, purpose string, expression query.Expression, policy dashboard.DashboardPolicy) *query.QueryPlan {
	canonicalKey := "query:" + purpose + ":kafka:" + family.Key
	metadata := query.QueryMetadata{
		Kind: query.QueryKindRate, CanonicalKey: canonicalKey,
		PlanIDs:      append([]string(nil), family.PlanIDs...),
		Provenance:   []string{"kafka:families[" + family.Key + "]"},
		RateInterval: policy.RateInterval,
		HashVersion:  dashboard.HashVersion,
	}
	switch purpose {
	case "error_ratio":
		metadata.Kind = query.QueryKindErrorRatio
		metadata.ErrorStatusPattern = query.ErrorStatusPattern
	case "p50", "p95", "p99":
		metadata.Kind = query.QueryKindPercentile
		metadata.Quantiles = []float64{quantileForPurpose(purpose)}
	}
	return &query.QueryPlan{
		CanonicalKey: canonicalKey, Kind: metadata.Kind, ItemID: "kafka:" + family.Key,
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

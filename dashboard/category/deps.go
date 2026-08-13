package category

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

// dependencyClass is one P2-09 row: a single dependency kind with a
// controlled row and panel identity that never collides with the P2-07
// server endpoint rows.
type dependencyClass struct {
	category       dashboard.Category
	dependencyKind string
	// rowPurpose separates the client rows from the server rows that
	// share the HTTP/RPC category ("panels" vs "clients"); it is part of
	// every row ID key.
	rowPurpose  string
	rowTitle    string
	description string
	// classSlot is the stable ID slot used by the P2-04 panel and table
	// keys; titleOperation is the controlled PanelTitle operation slot.
	classSlot      string
	titleOperation string
	// label is the stable canonical-key and provenance segment.
	label string
	// rateTitle is the controlled rate panel title ("operation rate",
	// "command rate", "request rate", "call rate").
	rateTitle string
	// noun is the controlled class adjective used in static descriptions
	// ("database", "cache", "HTTP client", "RPC client"); it never echoes
	// an IR value.
	noun string
}

// dependencyClasses is the fixed P2-09 row set in canonical order.
var dependencyClasses = []dependencyClass{
	{
		category: dashboard.CategoryDatabase, dependencyKind: "sql",
		rowPurpose: "panels", rowTitle: "Database",
		description: "SQL database calls identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
		classSlot: "sql", titleOperation: "", label: "database",
		rateTitle: "operation rate", noun: "database",
	},
	{
		category: dashboard.CategoryCache, dependencyKind: "redis",
		rowPurpose: "panels", rowTitle: "Cache",
		description: "Redis cache calls identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
		classSlot: "redis", titleOperation: "", label: "cache",
		rateTitle: "command rate", noun: "cache",
	},
	{
		category: dashboard.CategoryHTTP, dependencyKind: "http_client",
		rowPurpose: "clients", rowTitle: "HTTP Client Calls",
		description: "HTTP client calls identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
		classSlot: "client", titleOperation: "client", label: "http_client",
		rateTitle: "request rate", noun: "HTTP client",
	},
	{
		category: dashboard.CategoryRPC, dependencyKind: "rpc_client",
		rowPurpose: "clients", rowTitle: "RPC Client Calls",
		description: "RPC client calls identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
		classSlot: "client", titleOperation: "client", label: "rpc_client",
		rateTitle: "call rate", noun: "RPC client",
	},
}

// BuildDependencies plans the P2-09 rows: Database (SQL), Cache (Redis)
// and the controlled HTTP/RPC client-call subsections. Only dependency
// items whose DependencyKind matches enter; raw SQL text, Redis keys and
// values, URLs, RPC targets and PII never become labels, titles, legends
// or link parameters. Client rows are gated on
// policy.IncludeClientDependencies and always stay distinct from the
// P2-07 server endpoint rows. Catalog-level diagnostics (unsupported or
// policy-blocked targets) are preserved.
func BuildDependencies(catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) (*Plan, error) {
	if catalog == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "catalog", Message: "catalog is nil",
		}
	}
	serviceName, diagnostics := validateServiceName(catalog.ServiceName)
	diagnostics = append(diagnostics, catalog.Diagnostics...)

	plan := &Plan{}
	for _, class := range dependencyClasses {
		if isClientClass(class) && !policy.IncludeClientDependencies {
			continue
		}
		row, err := buildDependencyRow(catalog, class, serviceName, policy, &diagnostics)
		if err != nil {
			return nil, err
		}
		plan.Rows = append(plan.Rows, row)
	}
	if diagnostics == nil {
		diagnostics = []dashboard.Diagnostic{}
	}
	sortDiagnostics(diagnostics)
	plan.Diagnostics = diagnostics
	return plan, nil
}

// isClientClass reports whether the class is an HTTP/RPC client-call
// subsection (gated on policy.IncludeClientDependencies).
func isClientClass(class dependencyClass) bool {
	return class.dependencyKind == "http_client" || class.dependencyKind == "rpc_client"
}

func buildDependencyRow(catalog *dashboard.DashboardCatalog, class dependencyClass, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) (RowPlan, error) {
	items := dependencyItems(catalog.Items, class)
	row := RowPlan{
		Category:    class.category,
		Title:       class.rowTitle,
		Description: class.description,
		RowKey:      dashboard.RowIDKey(class.category, class.rowPurpose),
	}
	if len(items) == 0 {
		*diagnostics = append(*diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(class.category),
			Field:   "rows." + string(class.category) + "." + class.rowPurpose,
			Message: "no " + class.dependencyKind + " dependency entities; the row is omitted",
		})
		return row, nil
	}

	families := familyGroups(items, diagnostics)
	links := familyTraceLinks(items, serviceName, policy, diagnostics)

	specs := []struct {
		purpose string
		targets func() ([]Target, error)
	}{
		{purpose: "rate", targets: func() ([]Target, error) {
			return familyTargets(families, func(capabilities familyCapabilities) bool { return capabilities.Rate },
				func(family *metricFamily, svc string, pol dashboard.DashboardPolicy) (*query.QueryPlan, string) {
					return familyRateQuery(family, class.label, svc, pol)
				}, serviceName, policy)
		}},
		{purpose: "error_ratio", targets: func() ([]Target, error) {
			return familyTargets(families, func(capabilities familyCapabilities) bool { return capabilities.ErrorRatio },
				func(family *metricFamily, svc string, pol dashboard.DashboardPolicy) (*query.QueryPlan, string) {
					return familyErrorRatioQuery(family, class.label, svc, pol)
				}, serviceName, policy)
		}},
		{purpose: "p50", targets: func() ([]Target, error) {
			return familyPercentileTargets(families, class.label, 0.50, serviceName, policy)
		}},
		{purpose: "p95", targets: func() ([]Target, error) {
			return familyPercentileTargets(families, class.label, 0.95, serviceName, policy)
		}},
		{purpose: "p99", targets: func() ([]Target, error) {
			return familyPercentileTargets(families, class.label, 0.99, serviceName, policy)
		}},
	}
	var panels []Panel
	total := 0
	for _, spec := range specs {
		targets, err := spec.targets()
		if err != nil {
			return RowPlan{}, err
		}
		if len(targets) == 0 {
			*diagnostics = append(*diagnostics, missingDepsPanelDiagnostic(class, spec.purpose))
			continue
		}
		panel := depsPanel(class, spec.purpose, targets)
		panel.Links = links
		panels = append(panels, panel)
		total += len(targets)
	}

	table, err := depsOperationTable(items, class, serviceName, policy, diagnostics)
	if err != nil {
		return RowPlan{}, err
	}
	if len(table.Targets) > 0 {
		table.Links = links
		panels = append(panels, table)
		total += len(table.Targets)
	} else {
		*diagnostics = append(*diagnostics, missingDepsPanelDiagnostic(class, "operations"))
	}

	sort.Slice(panels, func(left, right int) bool { return panels[left].Key < panels[right].Key })
	if len(panels) > MaxPanelsPerCategory {
		return RowPlan{}, limitError(class.category, class.rowPurpose+".panels", len(panels), MaxPanelsPerCategory)
	}
	if total > MaxQueriesPerCategory {
		return RowPlan{}, limitError(class.category, class.rowPurpose+".queries", total, MaxQueriesPerCategory)
	}
	if len(panels) == 0 {
		*diagnostics = append(*diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(class.category),
			Field:   "rows." + string(class.category) + "." + class.rowPurpose,
			Message: "no generatable " + class.dependencyKind + " panels; the row is omitted",
		})
	}
	row.Panels = panels
	return row, nil
}

// dependencyItems selects the dependency items of one class. Client items
// are returned only when the catalog admitted them (P2-01 already gates
// them on policy.IncludeClientDependencies).
func dependencyItems(items []dashboard.DashboardItem, class dependencyClass) []dashboard.DashboardItem {
	var selected []dashboard.DashboardItem
	for _, item := range items {
		if item.Category == class.category && item.Target.Kind == "dependency" && item.DependencyKind == class.dependencyKind {
			selected = append(selected, item)
		}
	}
	return selected
}

func depsPanel(class dependencyClass, purpose string, targets []Target) Panel {
	spec := depsSpecForPurpose(class, purpose)
	return Panel{
		Key:         dashboard.PanelIDKey(class.category, class.classSlot, purpose),
		ItemID:      class.classSlot,
		Purpose:     purpose,
		Title:       dashboard.PanelTitle(class.category, class.titleOperation, spec.title),
		Description: spec.description,
		Type:        spec.panelType,
		Width:       spec.width,
		Height:      spec.height,
		Unit:        spec.unit,
		NoValue:     spec.noValue,
		Targets:     targets,
	}
}

// depsSpecForPurpose returns the fixed panel shape for one P2-09 purpose.
// All titles and descriptions are static controlled strings; none carry
// raw SQL, Redis, URL or RPC target values.
func depsSpecForPurpose(class dependencyClass, purpose string) panelSpec {
	switch purpose {
	case "rate":
		return panelSpec{
			title:       class.rateTitle,
			description: fmt.Sprintf("Rate of %s operations; requires runtime instrumentation from the Phase 1 Instrumentation Plan.", class.noun),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	case "error_ratio":
		return panelSpec{
			title:       "error ratio",
			description: fmt.Sprintf("Share of %s operations ending in the fixed error status pattern; requires runtime instrumentation.", class.noun),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "percent", noValue: "0", legend: "{{operation}}",
		}
	case "p50":
		return panelSpec{
			title:       "p50 duration",
			description: fmt.Sprintf("Median %s duration in seconds; requires runtime instrumentation.", class.noun),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p95":
		return panelSpec{
			title:       "p95 duration",
			description: fmt.Sprintf("95th percentile %s duration in seconds; requires runtime instrumentation.", class.noun),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p99":
		return panelSpec{
			title:       "p99 duration",
			description: fmt.Sprintf("99th percentile %s duration in seconds; requires runtime instrumentation.", class.noun),
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	default:
		return panelSpec{
			title:       "operations",
			description: fmt.Sprintf("Operation rate breakdown across the %s counter metric families; requires runtime instrumentation.", class.noun),
			panelType:   model.PanelTypeTable, width: dashboard.PanelWidthTable, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	}
}

func missingDepsPanelDiagnostic(class dependencyClass, purpose string) dashboard.Diagnostic {
	return dashboard.Diagnostic{
		Code: dashboard.CodeMissingRequiredMetric, TargetID: string(class.category) + ":" + class.classSlot,
		Field:   "rows." + string(class.category) + "." + class.rowPurpose + "." + class.classSlot + "." + purpose,
		Message: "no " + class.dependencyKind + " metric family supports this panel; the panel is omitted",
	}
}

func depsOperationTable(items []dashboard.DashboardItem, class dependencyClass, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) (Panel, error) {
	targets, err := operationTableTargets(items, serviceName, policy, diagnostics)
	if err != nil {
		return Panel{}, err
	}
	if len(targets) > model.MaxTargetsPerPanel {
		return Panel{}, &dashboard.CatalogError{
			Code:    dashboard.CodePanelLimitExceeded,
			Field:   "rows." + string(class.category) + "." + class.rowPurpose + "." + class.classSlot + ".operations.targets",
			Message: fmt.Sprintf("%s operation table exceeds the fixed limit of %d targets", class.dependencyKind, model.MaxTargetsPerPanel),
		}
	}
	panel := depsPanel(class, "operations", targets)
	return panel, nil
}

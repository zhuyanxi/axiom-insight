package category

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

const (
	// MaxPanelsPerCategory is the P2-07 per-row panel ceiling.
	MaxPanelsPerCategory = 60
	// MaxQueriesPerCategory is the P2-07 per-row query ceiling.
	MaxQueriesPerCategory = 150
	// RowPurpose is the fixed row-ID purpose of category rows.
	RowPurpose = "panels"
	// operationTableItemID is the synthetic item slot of the operation
	// breakdown table in the P2-04 ID keys.
	operationTableItemID = "operations"
)

// Plan is the typed P2-07 output: HTTP then RPC rows plus row-level
// diagnostics.
type Plan struct {
	// Rows hold the HTTP and RPC row plans; rows without panels are
	// omitted by Render.
	Rows []RowPlan
	// Diagnostics are the sorted, stable row diagnostics.
	Diagnostics []dashboard.Diagnostic
}

// RowPlan is one endpoint category row.
type RowPlan struct {
	// Category is http or rpc.
	Category dashboard.Category
	// Title is the fixed category title.
	Title string
	// Description is the static row description.
	Description string
	// RowKey is the deterministic row ID key (P2-04).
	RowKey string
	// Panels are the row's panels in canonical order.
	Panels []Panel
}

// Panel is one endpoint or table panel with its targets and links.
type Panel struct {
	// Key is the deterministic panel ID key (P2-04).
	Key string
	// ItemID is the owning catalog item ID (or the synthetic table slot).
	ItemID string
	// Purpose is the fixed panel purpose, e.g. "rate" or "operations".
	Purpose string
	// Title and Description are static controlled display strings.
	Title       string
	Description string
	// Type is stat or table.
	Type string
	// Width and Height enter the P2-04 grid.
	Width  int
	Height int
	// Unit and NoValue enter fieldConfig.defaults.
	Unit    string
	NoValue string
	// Targets are the rendered query targets.
	Targets []Target
	// Links are the controlled trace links of the owning item.
	Links []model.Link
}

// Target is one rendered query target with legacy per-item metadata.
type Target struct {
	// CanonicalKey is the deterministic query identity.
	CanonicalKey string
	// Kind is the query kind, e.g. "rate" or "percentile".
	Kind string
	// PlanID is the source GenerationPlan item ID.
	PlanID string
	// TargetID is the IR entity ID.
	TargetID string
	// Expr is the rendered expression.
	Expr string
	// LegendFormat is the controlled legend template.
	LegendFormat string
}

// Build plans the HTTP and RPC rows for one validated catalog. It never
// modifies the catalog. Structural failures (nil catalog, query-limit or
// panel-limit violations, a query failing P2-05 validation) return an
// error and no partial plan.
func Build(catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) (*Plan, error) {
	if catalog == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "catalog", Message: "catalog is nil",
		}
	}
	serviceName, diagnostics := validateServiceName(catalog.ServiceName)
	plan := &Plan{Diagnostics: diagnostics}
	for _, category := range []dashboard.Category{dashboard.CategoryHTTP, dashboard.CategoryRPC} {
		row, err := buildRow(catalog, category, serviceName, policy, &plan.Diagnostics)
		if err != nil {
			return nil, err
		}
		plan.Rows = append(plan.Rows, row)
	}
	if plan.Diagnostics == nil {
		plan.Diagnostics = []dashboard.Diagnostic{}
	}
	sortDiagnostics(plan.Diagnostics)
	return plan, nil
}

func buildRow(catalog *dashboard.DashboardCatalog, category dashboard.Category, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) (RowPlan, error) {
	row := RowPlan{
		Category:    category,
		Title:       dashboard.CategoryTitle(category),
		Description: rowDescription(category),
		RowKey:      dashboard.RowIDKey(category, RowPurpose),
	}
	items := endpointItems(catalog.Items, category)
	if len(items) == 0 {
		*diagnostics = append(*diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(category),
			Field:   "rows." + string(category),
			Message: "no " + string(category) + " endpoint entities; the row is omitted",
		})
		return row, nil
	}

	var panels []Panel
	for _, item := range items {
		itemPanels, err := itemPanels(item, category, serviceName, policy, diagnostics)
		if err != nil {
			return RowPlan{}, err
		}
		panels = append(panels, itemPanels...)
	}
	table, err := operationTable(items, category, serviceName, policy, diagnostics)
	if err != nil {
		return RowPlan{}, err
	}
	if len(table.Targets) > 0 {
		panels = append(panels, table)
	}
	sort.Slice(panels, func(left, right int) bool { return panels[left].Key < panels[right].Key })

	if len(panels) > MaxPanelsPerCategory {
		return RowPlan{}, limitError(category, "panels", len(panels), MaxPanelsPerCategory)
	}
	totalQueries := 0
	for _, panel := range panels {
		totalQueries += len(panel.Targets)
	}
	if totalQueries > MaxQueriesPerCategory {
		return RowPlan{}, limitError(category, "queries", totalQueries, MaxQueriesPerCategory)
	}

	if len(panels) == 0 {
		*diagnostics = append(*diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: string(category),
			Field:   "rows." + string(category),
			Message: "no generatable panels for " + string(category) + "; the row is omitted",
		})
	}
	row.Panels = panels
	return row, nil
}

// endpointItems returns the handler endpoints of one category in catalog
// order. Client dependencies stay out of P2-07 scope (P2-09 owns them).
func endpointItems(items []dashboard.DashboardItem, category dashboard.Category) []dashboard.DashboardItem {
	var selected []dashboard.DashboardItem
	for _, item := range items {
		if item.Category == category && item.Target.Kind == "endpoint" {
			selected = append(selected, item)
		}
	}
	return selected
}

func itemPanels(item dashboard.DashboardItem, category dashboard.Category, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) ([]Panel, error) {
	plans, links, itemDiagnostics := query.PlanItemQueries(&item, serviceName, policy)
	*diagnostics = append(*diagnostics, itemDiagnostics...)
	var panels []Panel
	for _, plan := range plans {
		panel, err := panelFromPlan(plan, item, category, serviceName)
		if err != nil {
			return nil, err
		}
		panel.Links = traceLinks(links, &item, policy)
		panels = append(panels, panel)
	}
	return panels, nil
}

func panelFromPlan(plan query.QueryPlan, item dashboard.DashboardItem, category dashboard.Category, serviceName string) (Panel, error) {
	if violations := query.ValidatePlan(&plan, &item, serviceName); len(violations) > 0 {
		return Panel{}, &dashboard.CatalogError{
			Code:    dashboard.CodeRenderError,
			Field:   "queries[" + plan.CanonicalKey + "]",
			Message: fmt.Sprintf("endpoint query failed P2-05 validation: %v", violations[0]),
		}
	}
	expression, err := query.Render(plan.Expression)
	if err != nil {
		return Panel{}, &dashboard.CatalogError{
			Code:    dashboard.CodeRenderError,
			Field:   "queries[" + plan.CanonicalKey + "]",
			Message: err.Error(),
		}
	}
	spec := specForPurpose(plan.Purpose)
	planID := ""
	if len(plan.PlanIDs) > 0 {
		planID = plan.PlanIDs[0]
	}
	return Panel{
		Key:         dashboard.PanelIDKey(category, item.ID, plan.Purpose),
		ItemID:      item.ID,
		Purpose:     plan.Purpose,
		Title:       dashboard.PanelTitle(category, item.Operation, spec.title),
		Description: spec.description,
		Type:        spec.panelType,
		Width:       spec.width,
		Height:      spec.height,
		Unit:        spec.unit,
		NoValue:     spec.noValue,
		Targets: []Target{{
			CanonicalKey: plan.CanonicalKey,
			Kind:         string(plan.Kind),
			PlanID:       planID,
			TargetID:     item.ID,
			Expr:         expression,
			LegendFormat: spec.legend,
		}},
	}, nil
}

func traceLinks(links []query.TraceLinkPlan, item *dashboard.DashboardItem, policy dashboard.DashboardPolicy) []model.Link {
	var result []model.Link
	for _, link := range links {
		if violations := query.ValidateTraceLink(&link, item, policy); len(violations) > 0 {
			continue
		}
		result = append(result, model.Link{
			Title:       "Traces",
			URL:         traceLinkURL(link),
			TargetBlank: true,
		})
	}
	return result
}

func validateServiceName(value string) (string, []dashboard.Diagnostic) {
	if query.ValidServiceValue(value) {
		return value, nil
	}
	return "", []dashboard.Diagnostic{{
		Code: dashboard.CodeSensitiveValueDropped, TargetID: "service",
		Field: "service.name", Message: "service name is not a controlled value; service matchers were dropped",
	}}
}

func rowDescription(category dashboard.Category) string {
	switch category {
	case dashboard.CategoryHTTP:
		return "HTTP endpoints identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation."
	default:
		return "gRPC endpoints identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation."
	}
}

func limitError(category dashboard.Category, kind string, count, limit int) error {
	return &dashboard.CatalogError{
		Code:    dashboard.CodePanelLimitExceeded,
		Field:   "rows." + string(category) + "." + kind,
		Message: fmt.Sprintf("%s row %s count %d exceeds the fixed limit of %d", string(category), kind, count, limit),
	}
}

func sortDiagnostics(diagnostics []dashboard.Diagnostic) {
	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].Code != diagnostics[right].Code {
			return diagnostics[left].Code < diagnostics[right].Code
		}
		if diagnostics[left].TargetID != diagnostics[right].TargetID {
			return diagnostics[left].TargetID < diagnostics[right].TargetID
		}
		return diagnostics[left].Field < diagnostics[right].Field
	})
}

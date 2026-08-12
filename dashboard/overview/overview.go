package overview

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

const (
	// PanelKeyItemID is the fixed synthetic item ID used by the overview
	// panel and row ID keys (P2-04): an overview panel aggregates many
	// catalog items, so it occupies one stable ID slot.
	PanelKeyItemID = "overview"
	// MaxOverviewQueries is the P2-06 overview query ceiling. Exceeding
	// it fails with DASHBOARD_PANEL_LIMIT_EXCEEDED; queries are never
	// silently dropped.
	MaxOverviewQueries = 30
)

// Plan is the typed, fully traceable P2-06 output. Render converts it
// into the P2-02 model; every target keeps its family references so the
// renderer and future runbook consumers can trace back to categories,
// items and GenerationPlan IDs.
type Plan struct {
	// DatasourceVariable is always present.
	DatasourceVariable model.Variable
	// OperationVariable is present only when the catalog declares at
	// least two valid operations.
	OperationVariable *model.Variable
	// Panels are the generatable overview panels in the fixed canonical
	// order; absent capabilities omit the panel.
	Panels []Panel
	// Diagnostics are the sorted, stable overview diagnostics.
	Diagnostics []dashboard.Diagnostic
}

// Panel is one overview panel with its family targets.
type Panel struct {
	// Key is the deterministic panel ID key (P2-04).
	Key string
	// Purpose is the fixed panel purpose, e.g. "requests_rate" or "p95".
	Purpose string
	// Title and Description are static controlled display strings.
	Title       string
	Description string
	// Type is stat or table for the v1 overview panel set.
	Type string
	// Width and Height enter the P2-04 grid.
	Width  int
	Height int
	// Unit and NoValue enter fieldConfig.defaults.
	Unit    string
	NoValue string
	// Targets are one per aggregated metric family.
	Targets []Target
}

// Target is one overview query target. Expr is the rendered controlled
// PromQL; Query keeps the typed P2-05 plan for validation and rendering.
type Target struct {
	// CanonicalKey is the deterministic query identity.
	CanonicalKey string
	// Kind is the query kind, e.g. "rate" or "top_failing".
	Kind string
	// Categories, ItemIDs and PlanIDs are the sorted, deduplicated family
	// references.
	Categories []dashboard.Category
	ItemIDs    []string
	PlanIDs    []string
	// Expr is the rendered expression.
	Expr string
	// LegendFormat is the controlled legend template.
	LegendFormat string
	// Query is the typed P2-05 query plan.
	Query *query.QueryPlan
}

// Build plans the Service Overview for one validated catalog. It never
// modifies the catalog. Structural failures (nil catalog, an overview
// query failing P2-05 validation, or the query ceiling) return an error
// and no partial plan; capability gaps and empty categories become
// diagnostics.
func Build(catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) (*Plan, error) {
	if catalog == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "catalog", Message: "catalog is nil",
		}
	}
	serviceName, diagnostics := validateServiceName(catalog.ServiceName)
	families := buildFamilies(catalog, &diagnostics)
	operationVariable := operationVariable(catalog.Items, &diagnostics)

	builder := panelBuilder{
		serviceName: serviceName,
		policy:      policy,
		families:    families,
		hasItems:    len(catalog.Items) > 0,
		diagnostics: &diagnostics,
	}
	panels, err := builder.panels()
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		DatasourceVariable: datasourceVariable(policy),
		OperationVariable:  operationVariable,
		Panels:             panels,
		Diagnostics:        diagnostics,
	}
	if len(panels) == 0 {
		plan.Diagnostics = append(plan.Diagnostics, dashboard.Diagnostic{
			Code: dashboard.CodeEmptyCategory, TargetID: PanelKeyItemID,
			Field: "overview.panels", Message: "no overview panel can be generated; the overview row is omitted",
		})
	}
	if plan.Diagnostics == nil {
		plan.Diagnostics = []dashboard.Diagnostic{}
	}
	sortDiagnostics(plan.Diagnostics)
	return plan, nil
}

// validateServiceName returns the validated service matcher value; an
// uncontrolled name is dropped with a diagnostic and an empty value so no
// raw value reaches a selector.
func validateServiceName(value string) (string, []dashboard.Diagnostic) {
	if query.ValidServiceValue(value) {
		return value, nil
	}
	return "", []dashboard.Diagnostic{{
		Code: dashboard.CodeSensitiveValueDropped, TargetID: "service",
		Field: "service.name", Message: "service name is not a controlled value; service matchers were dropped",
	}}
}

// sortDiagnostics orders diagnostics by code, target ID then field.
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

func queryLimitError(count int) error {
	return &dashboard.CatalogError{
		Code: dashboard.CodePanelLimitExceeded, Field: "overview.queries",
		Message: fmt.Sprintf("overview query count %d exceeds the fixed limit of %d", count, MaxOverviewQueries),
	}
}

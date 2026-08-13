package pipeline

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/category"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/overview"
)

// Plan is the immutable P2-10 assembly plan. It is produced by Build,
// never modified afterwards, and consumed by Render and the CLI report.
// The slices are owned by the plan; callers read them through the
// accessors, which return defensive copies for the mutable values.
type Plan struct {
	serviceName  string
	title        string
	uid          string
	policyDigest string
	timezone     string
	refresh      string
	variables    []model.Variable
	rows         []model.Row
	diagnostics  []dashboard.Diagnostic
}

// ServiceName is the validated service name.
func (plan *Plan) ServiceName() string { return plan.serviceName }

// Title is the composed dashboard title.
func (plan *Plan) Title() string { return plan.title }

// UID is the deterministic dashboard UID.
func (plan *Plan) UID() string { return plan.uid }

// PolicyDigest is the source policy digest (P2-03); it is reported by
// the CLI but never written into the dashboard JSON.
func (plan *Plan) PolicyDigest() string { return plan.policyDigest }

// Timezone is the dashboard timezone.
func (plan *Plan) Timezone() string { return plan.timezone }

// Refresh is the dashboard refresh interval.
func (plan *Plan) Refresh() string { return plan.refresh }

// Variables returns a copy of the controlled templating variables.
func (plan *Plan) Variables() []model.Variable {
	return deepCopyVariables(plan.variables)
}

// Rows returns a copy of the stacked category rows.
func (plan *Plan) Rows() []model.Row {
	return deepCopyRows(plan.rows)
}

// Diagnostics returns a copy of the sorted, deduplicated diagnostics.
func (plan *Plan) Diagnostics() []dashboard.Diagnostic {
	return append([]dashboard.Diagnostic(nil), plan.diagnostics...)
}

// deepCopyVariables copies every variable including its pointer fields
// and option slices, so the returned slice never shares mutable storage
// with the immutable plan.
func deepCopyVariables(variables []model.Variable) []model.Variable {
	result := make([]model.Variable, len(variables))
	for index, variable := range variables {
		result[index] = variable
		if variable.Hide != nil {
			hide := *variable.Hide
			result[index].Hide = &hide
		}
		result[index].Datasource = deepCopyDatasource(variable.Datasource)
		result[index].Options = append([]model.VariableOption(nil), variable.Options...)
		if variable.Current != nil {
			current := *variable.Current
			result[index].Current = &current
		}
	}
	return result
}

// deepCopyRows copies every row and its nested panels, targets, links and
// field config so callers can never mutate the plan through an accessor.
func deepCopyRows(rows []model.Row) []model.Row {
	result := make([]model.Row, len(rows))
	for index, row := range rows {
		result[index] = row
		result[index].Panels = deepCopyPanels(row.Panels)
	}
	return result
}

func deepCopyPanels(panels []model.Panel) []model.Panel {
	result := make([]model.Panel, len(panels))
	for index, panel := range panels {
		result[index] = panel
		result[index].Datasource = deepCopyDatasource(panel.Datasource)
		result[index].Targets = deepCopyTargets(panel.Targets)
		result[index].Links = append([]model.Link(nil), panel.Links...)
		defaults := panel.FieldConfig.Defaults
		if defaults.Min != nil {
			min := *defaults.Min
			defaults.Min = &min
		}
		if defaults.Max != nil {
			max := *defaults.Max
			defaults.Max = &max
		}
		result[index].FieldConfig.Defaults = defaults
		result[index].FieldConfig.Overrides = deepCopyOverrides(panel.FieldConfig.Overrides)
	}
	return result
}

func deepCopyTargets(targets []model.Target) []model.Target {
	result := make([]model.Target, len(targets))
	for index, target := range targets {
		result[index] = target
		result[index].Datasource = deepCopyDatasource(target.Datasource)
		if target.Metadata != nil {
			metadata := *target.Metadata
			metadata.Categories = append([]string(nil), target.Metadata.Categories...)
			metadata.ItemIDs = append([]string(nil), target.Metadata.ItemIDs...)
			metadata.PlanIDs = append([]string(nil), target.Metadata.PlanIDs...)
			result[index].Metadata = &metadata
		}
	}
	return result
}

func deepCopyDatasource(datasource *model.DatasourceRef) *model.DatasourceRef {
	if datasource == nil {
		return nil
	}
	copy := *datasource
	return &copy
}

func deepCopyOverrides(overrides []model.FieldOverride) []model.FieldOverride {
	result := make([]model.FieldOverride, len(overrides))
	for index, override := range overrides {
		result[index] = override
		result[index].Properties = append([]model.FieldProperty(nil), override.Properties...)
	}
	return result
}

// Build runs the P2-06 overview, P2-07 HTTP/RPC, P2-08 Kafka and P2-09
// dependency builders over one validated catalog and policy, wraps the
// overview panels into the Service Overview row, stacks every non-empty
// row in canonical order, and returns an immutable Plan. Fatal
// structural failures (nil catalog, a sub-build failure, the policy
// panel/query ceilings, or an empty dashboard) return an error and no
// partial plan.
func Build(catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) (*Plan, error) {
	if catalog == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "catalog", Message: "catalog is nil",
		}
	}

	overviewPlan, err := overview.Build(catalog, policy)
	if err != nil {
		return nil, err
	}
	variables, overviewPanels, err := overview.Render(overviewPlan)
	if err != nil {
		return nil, err
	}

	httpRPCPlan, err := category.Build(catalog, policy)
	if err != nil {
		return nil, err
	}
	kafkaPlan, err := category.BuildKafka(catalog, policy)
	if err != nil {
		return nil, err
	}
	depsPlan, err := category.BuildDependencies(catalog, policy)
	if err != nil {
		return nil, err
	}

	rows, rowKeys, err := assembleRows(httpRPCPlan, kafkaPlan, depsPlan, overviewPanels)
	if err != nil {
		return nil, err
	}
	// Row IDs are re-resolved over the whole key set so they stay unique
	// across the independent sub-renderers and the overview row.
	if err := assignRowIDs(rows, rowKeys); err != nil {
		return nil, err
	}
	stackRows(rows)

	diagnostics := aggregateDiagnostics(catalog, overviewPlan, httpRPCPlan, kafkaPlan, depsPlan)
	if err := checkLimits(rows, policy); err != nil {
		return nil, err
	}
	if totalPanels(rows) == 0 {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeEmptyCategory, Field: "rows",
			Message: "no dashboard panel can be generated; the dashboard is empty",
		}
	}

	return &Plan{
		serviceName:  catalog.ServiceName,
		title:        dashboard.ComposeDashboardTitle(catalog.ServiceName, policy.TitleSuffix),
		uid:          dashboard.DashboardUID(catalog.ServiceName),
		policyDigest: policy.Digest(),
		timezone:     policy.Timezone,
		refresh:      policy.Refresh,
		variables:    variables,
		rows:         rows,
		diagnostics:  diagnostics,
	}, nil
}

// assembleRows renders the P2-07/08/09 plans and wraps the P2-06
// overview panels into the Service Overview row, keeping the canonical
// order: overview, HTTP, RPC, Kafka, Database, Cache, then the client
// subsections. The parallel key slice feeds the unique row-ID pass.
func assembleRows(httpRPC, kafka, deps *category.Plan, overviewPanels []model.Panel) ([]model.Row, []string, error) {
	rows := make([]model.Row, 0, 1+len(httpRPC.Rows)+len(kafka.Rows)+len(deps.Rows))
	keys := make([]string, 0, cap(rows))

	if len(overviewPanels) > 0 {
		rows = append(rows, model.Row{
			Title:       dashboard.CategoryTitle(dashboard.CategoryServiceOverview),
			Description: "Service-wide request, error and latency signals identified by the Phase 1 Instrumentation Plan; data requires runtime instrumentation.",
			Panels:      overviewPanels,
		})
		keys = append(keys, dashboard.RowIDKey(dashboard.CategoryServiceOverview, category.RowPurpose))
	}

	for _, plan := range []*category.Plan{httpRPC, kafka, deps} {
		rendered, err := category.Render(plan)
		if err != nil {
			return nil, nil, err
		}
		renderedIndex := 0
		for _, rowPlan := range plan.Rows {
			if len(rowPlan.Panels) == 0 {
				continue
			}
			if renderedIndex >= len(rendered) {
				return nil, nil, fmt.Errorf("pipeline: renderer returned fewer rows than planned")
			}
			rows = append(rows, rendered[renderedIndex])
			keys = append(keys, rowPlan.RowKey)
			renderedIndex++
		}
	}
	return rows, keys, nil
}

// assignRowIDs re-resolves every row ID from its canonical key in one
// domain, so no two rows (including the overview row) share an ID. A
// length mismatch is a structural failure and surfaces at the assembly
// boundary instead of leaving zero IDs for the model validator to trip on.
func assignRowIDs(rows []model.Row, keys []string) error {
	if len(rows) != len(keys) {
		return &dashboard.CatalogError{
			Code: dashboard.CodeRenderError, Field: "rows",
			Message: fmt.Sprintf("row count %d does not match the row key set %d", len(rows), len(keys)),
		}
	}
	ids := dashboard.ResolvePanelIDs(keys)
	for index := range rows {
		rows[index].ID = int(ids[index])
	}
	return nil
}

// stackRows offsets every row's nested panels below its row band so the
// rows stack top-to-bottom without overlapping. Each row's panels arrive
// with relative Y starting at 0; the first row band sits at Y=0.
func stackRows(rows []model.Row) {
	cursorY := 0
	for index := range rows {
		if len(rows[index].Panels) == 0 {
			continue
		}
		maxBottom := 0
		for panelIndex := range rows[index].Panels {
			panel := &rows[index].Panels[panelIndex]
			panel.GridPos.Y += cursorY + 1
			bottom := panel.GridPos.Y + panel.GridPos.H
			if bottom > maxBottom {
				maxBottom = bottom
			}
		}
		cursorY = maxBottom
	}
}

// aggregateDiagnostics merges the catalog and every sub-build diagnostic,
// then sorts them (code, target ID, field) and removes exact duplicates.
func aggregateDiagnostics(
	catalog *dashboard.DashboardCatalog,
	overviewPlan *overview.Plan,
	httpRPC, kafka, deps *category.Plan,
) []dashboard.Diagnostic {
	var diagnostics []dashboard.Diagnostic
	diagnostics = append(diagnostics, catalog.Diagnostics...)
	diagnostics = append(diagnostics, overviewPlan.Diagnostics...)
	diagnostics = append(diagnostics, httpRPC.Diagnostics...)
	diagnostics = append(diagnostics, kafka.Diagnostics...)
	diagnostics = append(diagnostics, deps.Diagnostics...)

	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].Code != diagnostics[right].Code {
			return diagnostics[left].Code < diagnostics[right].Code
		}
		if diagnostics[left].TargetID != diagnostics[right].TargetID {
			return diagnostics[left].TargetID < diagnostics[right].TargetID
		}
		if diagnostics[left].Field != diagnostics[right].Field {
			return diagnostics[left].Field < diagnostics[right].Field
		}
		return diagnostics[left].Message < diagnostics[right].Message
	})
	unique := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		if len(unique) > 0 && unique[len(unique)-1] == diagnostic {
			continue
		}
		unique = append(unique, diagnostic)
	}
	return unique
}

// checkLimits fails with DASHBOARD_PANEL_LIMIT_EXCEEDED when the
// assembled dashboard exceeds the policy panel or query ceilings.
// Queries are never silently dropped.
func checkLimits(rows []model.Row, policy dashboard.DashboardPolicy) error {
	panels := totalPanels(rows)
	if int64(panels) > policy.MaxPanels {
		return &dashboard.CatalogError{
			Code: dashboard.CodePanelLimitExceeded, Field: "panels",
			Message: fmt.Sprintf("panel count %d exceeds the policy limit of %d", panels, policy.MaxPanels),
		}
	}
	queries := totalQueries(rows)
	if int64(queries) > policy.MaxQueries {
		return &dashboard.CatalogError{
			Code: dashboard.CodePanelLimitExceeded, Field: "queries",
			Message: fmt.Sprintf("query count %d exceeds the policy limit of %d", queries, policy.MaxQueries),
		}
	}
	return nil
}

// totalPanels counts every non-row panel (nested and top-level).
func totalPanels(rows []model.Row) int {
	count := 0
	for _, row := range rows {
		count += len(row.Panels)
	}
	return count
}

// totalQueries counts every query target across all panels.
func totalQueries(rows []model.Row) int {
	count := 0
	for _, row := range rows {
		for _, panel := range row.Panels {
			count += len(panel.Targets)
		}
	}
	return count
}

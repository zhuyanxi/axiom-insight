package overview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

func resolvePolicy(t *testing.T) dashboard.DashboardPolicy {
	t.Helper()
	policy, err := dashboard.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	return *policy
}

func metric(planID, name, metricType string, attributes ...string) dashboard.SignalReference {
	return dashboard.SignalReference{
		PlanID: planID, Name: name, Type: metricType, Attributes: attributes,
	}
}

func item(id string, category dashboard.Category, operation string, metrics ...dashboard.SignalReference) dashboard.DashboardItem {
	return dashboard.DashboardItem{
		ID: id, Category: category, Operation: operation, Metrics: metrics,
	}
}

func fullCatalog() *dashboard.DashboardCatalog {
	return &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			item("ep:http:get_user", dashboard.CategoryHTTP, "get",
				metric("m_http_count", "http_requests_total", "counter", "service", "operation", "status"),
				metric("m_http_dur", "http_request_duration", "histogram", "service", "operation"),
				metric("m_http_inf", "http_in_flight", "gauge", "service", "operation")),
			item("ep:http:create_order", dashboard.CategoryHTTP, "post",
				metric("m_http_count2", "http_requests_total", "counter", "service", "operation", "status"),
				metric("m_http_dur2", "http_request_duration", "histogram", "service", "operation"),
				metric("m_http_inf2", "http_in_flight", "gauge", "service", "operation")),
			item("ep:rpc:charge", dashboard.CategoryRPC, "charge",
				metric("m_rpc_count", "rpc_requests_total", "counter", "service", "operation", "status"),
				metric("m_rpc_dur", "rpc_request_duration", "histogram", "service", "operation")),
			item("dep:kafka:orders", dashboard.CategoryKafka, "produce",
				metric("m_kafka_count", "kafka_messages_total", "counter", "service", "operation", "status"),
				metric("m_kafka_dur", "kafka_message_duration", "histogram", "service", "operation")),
			item("dep:sql:orders", dashboard.CategoryDatabase, "query",
				metric("m_sql_count", "sql_queries_total", "counter", "service", "operation", "status")),
			item("dep:redis:cache", dashboard.CategoryCache, "get",
				metric("m_redis_count", "redis_commands_total", "counter", "service", "operation", "status")),
		},
	}
}

// TestBuildFullOverview is AC1: the composite service produces the
// datasource variable, the controlled operation variable and every
// applicable overview panel; every target is P2-05 validated inside
// Build.
func TestBuildFullOverview(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	wantPurposes := []string{
		"requests_rate", "error_ratio", "p50", "p95", "p99", "in_flight", "top_failing",
	}
	if len(plan.Panels) != len(wantPurposes) {
		t.Fatalf("panel count = %d, want %d", len(plan.Panels), len(wantPurposes))
	}
	for index, purpose := range wantPurposes {
		if plan.Panels[index].Purpose != purpose {
			t.Errorf("panel %d purpose = %q, want %q", index, plan.Panels[index].Purpose, purpose)
		}
	}

	if plan.DatasourceVariable.Name != "datasource" ||
		plan.DatasourceVariable.Type != model.VariableTypeDatasource ||
		plan.DatasourceVariable.Query != "prometheus" {
		t.Errorf("datasource variable = %+v", plan.DatasourceVariable)
	}
	if plan.DatasourceVariable.Hide == nil || *plan.DatasourceVariable.Hide != 0 {
		t.Errorf("datasource variable hide = %v, want 0", plan.DatasourceVariable.Hide)
	}

	if plan.OperationVariable == nil {
		t.Fatal("operation variable is nil")
	}
	var options []string
	for _, option := range plan.OperationVariable.Options {
		options = append(options, option.Value)
	}
	wantOperations := []string{"charge", "get", "post", "produce", "query"}
	if !reflect.DeepEqual(options, wantOperations) {
		t.Errorf("operation options = %v, want %v", options, wantOperations)
	}
	if !plan.OperationVariable.Multi || !plan.OperationVariable.IncludeAll {
		t.Errorf("operation variable flags = multi:%v includeAll:%v", plan.OperationVariable.Multi, plan.OperationVariable.IncludeAll)
	}

	// Rate, error ratio and top failing each have five counter families;
	// percentiles three histogram families; in-flight one gauge family.
	wantTargets := map[string]int{
		"requests_rate": 5, "error_ratio": 5, "p50": 3, "p95": 3, "p99": 3,
		"in_flight": 1, "top_failing": 5,
	}
	for _, panel := range plan.Panels {
		if want := wantTargets[panel.Purpose]; len(panel.Targets) != want {
			t.Errorf("panel %s targets = %d, want %d", panel.Purpose, len(panel.Targets), want)
		}
	}

	variables, panels, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(variables) != 2 || len(panels) != 7 {
		t.Fatalf("rendered variables/panels = %d/%d, want 2/7", len(variables), len(panels))
	}
	validateRenderedDashboard(t, variables, panels)
}

func validateRenderedDashboard(t *testing.T, variables []model.Variable, panels []model.Panel) {
	t.Helper()
	dashboard := &model.Dashboard{
		SchemaVersion: model.SchemaVersion,
		Title:         "payment Observability",
		UID:           "payment-uid",
		ID:            nil,
		Version:       0,
		Editable:      true,
		Timezone:      "browser",
		Refresh:       "30s",
		Templating:    model.Templating{List: variables},
		Rows:          []model.Row{{ID: 1, Title: "Service Overview", Panels: panels}},
		Annotations:   model.Annotations{},
	}
	if violations := model.Validate(dashboard); len(violations) > 0 {
		t.Fatalf("rendered overview fails model validation: %v", violations)
	}
	if _, err := model.Render(dashboard); err != nil {
		t.Fatalf("rendered overview fails model render: %v", err)
	}
}

// TestCounterOnlyOverview is AC2: a counter-only service generates only
// the rate, error ratio and top-failing panels; percentiles and in-flight
// are omitted with diagnostics.
func TestCounterOnlyOverview(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			item("ep:http:get", dashboard.CategoryHTTP, "get",
				metric("m_count", "http_requests_total", "counter", "service", "operation", "status")),
		},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	var purposes []string
	for _, panel := range plan.Panels {
		purposes = append(purposes, panel.Purpose)
	}
	want := []string{"requests_rate", "error_ratio", "top_failing"}
	if !reflect.DeepEqual(purposes, want) {
		t.Errorf("panels = %v, want %v", purposes, want)
	}
	for _, field := range []string{"overview.panels.p50", "overview.panels.p95", "overview.panels.p99", "overview.panels.in_flight"} {
		if !hasDiagnostic(plan.Diagnostics, dashboard.CodeMissingRequiredMetric, field) {
			t.Errorf("missing diagnostic for %s", field)
		}
	}
	if plan.OperationVariable != nil {
		t.Errorf("operation variable must be omitted with a single operation")
	}
	variables, panels, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	validateRenderedDashboard(t, variables, panels)
}

// TestNoPanelsOverview is task 6: a service with no generatable signal
// omits the overview row and reports DASHBOARD_EMPTY_CATEGORY.
func TestNoPanelsOverview(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			item("ep:cron:run", dashboard.CategoryServiceOverview, "run"),
		},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(plan.Panels) != 0 {
		t.Fatalf("expected no panels, got %d", len(plan.Panels))
	}
	if !hasDiagnostic(plan.Diagnostics, dashboard.CodeEmptyCategory, "overview.panels") {
		t.Errorf("expected DASHBOARD_EMPTY_CATEGORY, got %v", plan.Diagnostics)
	}
}

// TestEmptyCatalogOverview: an empty catalog reports only the empty
// category, without per-panel missing-metric noise.
func TestEmptyCatalogOverview(t *testing.T) {
	plan, err := Build(&dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
	}, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(plan.Panels) != 0 || !hasDiagnostic(plan.Diagnostics, dashboard.CodeEmptyCategory, "overview.panels") {
		t.Fatalf("empty catalog must report only DASHBOARD_EMPTY_CATEGORY, got %v", plan.Diagnostics)
	}
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeMissingRequiredMetric {
			t.Errorf("empty catalog must not report missing-metric diagnostics: %v", diagnostic)
		}
	}
}

// TestOperationVariableThreshold pins the two-or-more rule: one unique
// operation yields no variable, two distinct operations yield sorted
// static options.
func TestOperationVariableThreshold(t *testing.T) {
	catalog := func(operations ...string) *dashboard.DashboardCatalog {
		items := make([]dashboard.DashboardItem, 0, len(operations))
		for index, operation := range operations {
			items = append(items, item("ep:"+itoa(index), dashboard.CategoryHTTP, operation,
				metric("m"+itoa(index), "http_requests_total", "counter", "service", "operation", "status")))
		}
		return &dashboard.DashboardCatalog{SchemaVersion: dashboard.CatalogSchemaVersion, ServiceName: "payment", Items: items}
	}
	plan, err := Build(catalog("get"), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if plan.OperationVariable != nil {
		t.Errorf("one operation must not create the variable")
	}

	plan, err = Build(catalog("post", "get"), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if plan.OperationVariable == nil {
		t.Fatal("two operations must create the variable")
	}
	values := make([]string, 0, len(plan.OperationVariable.Options))
	for _, option := range plan.OperationVariable.Options {
		values = append(values, option.Value)
	}
	if !reflect.DeepEqual(values, []string{"get", "post"}) {
		t.Errorf("operation options = %v, want [get post]", values)
	}
}

// TestCanarySafety is AC3: raw operation, service and metric values never
// reach variables, selectors or diagnostics.
func TestCanarySafety(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   `payment";drop table users;--`,
		Items: []dashboard.DashboardItem{
			item("ep:http:get", dashboard.CategoryHTTP, `get/../../etc?q="x"`,
				metric("m_count", `http_requests_total";alert(1)`, "counter", "service", "operation", "status"),
				metric("m_ok", "http_requests_total", "counter", "service", "operation", "status")),
		},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	hadSensitive := false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped {
			hadSensitive = true
		}
		for _, canary := range []string{"drop table", "alert(1)", "etc?q="} {
			if strings.Contains(diagnostic.Message, canary) || strings.Contains(diagnostic.Field, canary) {
				t.Errorf("diagnostic leaks canary %q: %+v", canary, diagnostic)
			}
		}
	}
	if !hadSensitive {
		t.Errorf("expected sensitive-value diagnostics, got %v", plan.Diagnostics)
	}
	if plan.OperationVariable != nil {
		t.Errorf("canary operation must not create the variable")
	}
	_, panels, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	for _, panel := range panels {
		for _, target := range panel.Targets {
			for _, canary := range []string{"drop table", "alert(1)", "etc?q="} {
				if strings.Contains(target.Expr, canary) {
					t.Errorf("query leaks canary %q: %s", canary, target.Expr)
				}
			}
		}
	}
}

// TestQueryLimit fails with DASHBOARD_PANEL_LIMIT_EXCEEDED instead of
// truncating when the overview exceeds 30 queries.
func TestQueryLimit(t *testing.T) {
	items := make([]dashboard.DashboardItem, 0, 31)
	for index := range 31 {
		name := "requests_" + pad3(index)
		items = append(items, item("ep:"+pad3(index), dashboard.CategoryHTTP, "op"+pad3(index),
			metric("m"+pad3(index), name, "counter", "service", "operation", "status")))
	}
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items:         items,
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err == nil {
		t.Fatalf("Build must fail over the query ceiling, got %d targets", totalTargets(plan))
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodePanelLimitExceeded {
		t.Fatalf("error = %v, want DASHBOARD_PANEL_LIMIT_EXCEEDED", err)
	}
}

// TestPermutationDeterminism reruns Build and Render on a permuted
// catalog and requires byte-identical rendered output and diagnostics.
func TestPermutationDeterminism(t *testing.T) {
	first := fullCatalog()
	second := fullCatalog()
	// Reverse item order and every metric list.
	for index := range second.Items {
		metrics := second.Items[index].Metrics
		for left, right := 0, len(metrics)-1; left < right; left, right = left+1, right-1 {
			metrics[left], metrics[right] = metrics[right], metrics[left]
		}
	}
	for left, right := 0, len(second.Items)-1; left < right; left, right = left+1, right-1 {
		second.Items[left], second.Items[right] = second.Items[right], second.Items[left]
	}

	firstPlan, err := Build(first, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build(first): %v", err)
	}
	secondPlan, err := Build(second, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build(second): %v", err)
	}
	if !reflect.DeepEqual(firstPlan.Diagnostics, secondPlan.Diagnostics) {
		t.Errorf("diagnostics differ after permutation:\n%v\n%v", firstPlan.Diagnostics, secondPlan.Diagnostics)
	}
	firstVariables, firstPanels, err := Render(firstPlan)
	if err != nil {
		t.Fatalf("Render(first): %v", err)
	}
	secondVariables, secondPanels, err := Render(secondPlan)
	if err != nil {
		t.Fatalf("Render(second): %v", err)
	}
	if !reflect.DeepEqual(firstVariables, secondVariables) || !reflect.DeepEqual(firstPanels, secondPanels) {
		t.Fatal("rendered overview differs after permutation")
	}
}

func totalTargets(plan *Plan) int {
	total := 0
	for _, panel := range plan.Panels {
		total += len(panel.Targets)
	}
	return total
}

func hasDiagnostic(diagnostics []dashboard.Diagnostic, code, field string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code && diagnostic.Field == field {
			return true
		}
	}
	return false
}

func pad3(value int) string {
	if value < 10 {
		return "00" + itoa(value)
	}
	if value < 100 {
		return "0" + itoa(value)
	}
	return itoa(value)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [16]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}

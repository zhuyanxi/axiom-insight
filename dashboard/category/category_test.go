package category

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

func span(planID, name string) dashboard.SignalReference {
	return dashboard.SignalReference{PlanID: planID, Name: name, Type: "server"}
}

func endpoint(id string, category dashboard.Category, operation string, metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.DashboardItem {
	item := dashboard.DashboardItem{
		ID: id, Category: category, Target: dashboard.TargetRef{Kind: "endpoint", ID: id},
		Operation: operation, Metrics: metrics, Spans: spans,
	}
	item.Capabilities = capabilitiesOf(metrics, spans)
	return item
}

func fullHTTP(id, operation string) dashboard.DashboardItem {
	return endpoint(id, dashboard.CategoryHTTP, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "http_requests_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "http_request_duration", "histogram", "service", "operation"),
			metric(id+":inf", "http_in_flight", "gauge", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "GET /"+operation)})
}

func fullRPC(id, operation string) dashboard.DashboardItem {
	return endpoint(id, dashboard.CategoryRPC, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "rpc_requests_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "rpc_request_duration", "histogram", "service", "operation"),
			metric(id+":inf", "rpc_in_flight", "gauge", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Charge/"+operation)})
}

func counterOnly(id string, category dashboard.Category, operation string) dashboard.DashboardItem {
	return endpoint(id, category, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "requests_total", "counter", "service", "operation", "status"),
		}, nil)
}

func capabilitiesOf(metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.Capabilities {
	var counters, histograms, gauges int
	hasStatus, hasServiceOperation := false, false
	for _, m := range metrics {
		switch m.Type {
		case "counter":
			counters++
		case "histogram":
			histograms++
		case "gauge":
			gauges++
		}
		if contains(m.Attributes, "status") {
			hasStatus = true
		}
		if contains(m.Attributes, "service") && contains(m.Attributes, "operation") {
			hasServiceOperation = true
		}
	}
	return dashboard.Capabilities{
		Rate:        dashboard.QueryCapability{Available: counters > 0 && hasServiceOperation},
		ErrorRatio:  dashboard.QueryCapability{Available: counters > 0 && hasStatus},
		Percentiles: dashboard.QueryCapability{Available: histograms > 0},
		InFlight:    dashboard.QueryCapability{Available: gauges > 0},
		TraceLink:   dashboard.QueryCapability{Available: len(spans) > 0},
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func fullCatalog() *dashboard.DashboardCatalog {
	return &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			fullHTTP("ep:http:get_user", "get"),
			fullHTTP("ep:http:create_order", "post"),
			fullRPC("ep:rpc:charge", "charge"),
			fullRPC("ep:rpc:refund", "refund"),
			// Client dependency must stay out of the HTTP row (P2-09 owns it).
			{
				ID: "dep:http:client", Category: dashboard.CategoryHTTP,
				Target:    dashboard.TargetRef{Kind: "dependency", ID: "dep:http:client"},
				Operation: "get",
				Metrics: []dashboard.SignalReference{
					metric("m_client", "client_http_requests_total", "counter", "service", "operation", "status"),
				},
			},
			{
				ID: "dep:sql:orders", Category: dashboard.CategoryDatabase,
				Target:    dashboard.TargetRef{Kind: "dependency", ID: "dep:sql:orders"},
				Operation: "query",
				Metrics: []dashboard.SignalReference{
					metric("m_sql", "sql_queries_total", "counter", "service", "operation", "status"),
				},
			},
		},
	}
}

// TestBuildFullRows is AC1: HTTP and RPC full-capability rows generate
// rate, error ratio, p50/p95/p99, in-flight, operation table and trace
// links; every query passes validation inside Build.
func TestBuildFullRows(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(plan.Rows))
	}
	httpRow, rpcRow := plan.Rows[0], plan.Rows[1]
	if httpRow.Category != dashboard.CategoryHTTP || rpcRow.Category != dashboard.CategoryRPC {
		t.Fatalf("row order = %s,%s, want http,rpc", httpRow.Category, rpcRow.Category)
	}
	// 2 endpoints x 6 panels + 1 operation table.
	if len(httpRow.Panels) != 13 || len(rpcRow.Panels) != 13 {
		t.Fatalf("panels = %d,%d, want 13,13", len(httpRow.Panels), len(rpcRow.Panels))
	}
	if httpRow.Description == "" || rpcRow.Description == "" {
		t.Error("row description must be present")
	}

	var tableTargets int
	for _, row := range plan.Rows {
		foundTable := false
		for _, panel := range row.Panels {
			if panel.Purpose == "operations" {
				foundTable = true
				tableTargets += len(panel.Targets)
				if len(panel.Targets) != 2 {
					t.Errorf("%s table targets = %d, want 2", row.Category, len(panel.Targets))
				}
			}
			if panel.Purpose == "rate" && len(panel.Links) != 1 {
				t.Errorf("%s rate panel trace links = %d, want 1", row.Category, len(panel.Links))
			}
		}
		if !foundTable {
			t.Errorf("%s row has no operation table", row.Category)
		}
	}
	if tableTargets != 4 {
		t.Errorf("total table targets = %d, want 4", tableTargets)
	}
	for _, row := range plan.Rows {
		for _, panel := range row.Panels {
			for _, target := range panel.Targets {
				if strings.Contains(target.Expr, "client_http_requests_total") {
					t.Errorf("client dependency leaked into %s row: %s", row.Category, target.Expr)
				}
			}
		}
	}

	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rendered rows = %d, want 2", len(rows))
	}
	validateRenderedDashboard(t, rows)
}

func validateRenderedDashboard(t *testing.T, rows []model.Row) {
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
		Templating: model.Templating{List: []model.Variable{{
			Name: "datasource", Type: model.VariableTypeDatasource, Query: "prometheus",
		}}},
		Rows:        rows,
		Annotations: model.Annotations{},
	}
	if violations := model.Validate(dashboard); len(violations) > 0 {
		t.Fatalf("rendered rows fail model validation: %v", violations)
	}
	if _, err := model.Render(dashboard); err != nil {
		t.Fatalf("rendered rows fail model render: %v", err)
	}
}

// TestCounterOnlyDegradation is AC3: counter-only endpoints generate only
// provable rate/error panels; percentiles, in-flight and trace links are
// omitted with diagnostics.
func TestCounterOnlyDegradation(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			counterOnly("ep:http:get", dashboard.CategoryHTTP, "get"),
			counterOnly("ep:rpc:charge", dashboard.CategoryRPC, "charge"),
		},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	for _, row := range plan.Rows {
		// 1 endpoint x 2 panels + 1 table.
		if len(row.Panels) != 3 {
			t.Errorf("%s panels = %d, want 3", row.Category, len(row.Panels))
		}
		for _, panel := range row.Panels {
			if contains([]string{"p50", "p95", "p99", "in_flight"}, panel.Purpose) {
				t.Errorf("%s generated unsupported panel %s", row.Category, panel.Purpose)
			}
			if len(panel.Links) != 0 {
				t.Errorf("%s generated trace link without SERVER span", row.Category)
			}
		}
	}
	foundMissing := false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeMissingRequiredMetric {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected capability degradation diagnostics, got %v", plan.Diagnostics)
	}
}

// TestNoEntities omits rows without endpoint entities and reports
// DASHBOARD_EMPTY_CATEGORY.
func TestNoEntities(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{{
			ID: "dep:sql:orders", Category: dashboard.CategoryDatabase,
			Target:    dashboard.TargetRef{Kind: "dependency", ID: "dep:sql:orders"},
			Operation: "query",
			Metrics: []dashboard.SignalReference{
				metric("m_sql", "sql_queries_total", "counter", "service", "operation", "status"),
			},
		}},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	for _, category := range []string{"http", "rpc"} {
		found := false
		for _, diagnostic := range plan.Diagnostics {
			if diagnostic.Code == dashboard.CodeEmptyCategory &&
				diagnostic.Field == "rows."+category {
				found = true
			}
		}
		if !found {
			t.Errorf("missing empty-category diagnostic for %s", category)
		}
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty rows must be omitted, got %d", len(rows))
	}
}

// TestCanarySafety is AC2: raw path/metadata-like values never reach
// titles, queries or links.
func TestCanarySafety(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   `payment";drop`,
		Items: []dashboard.DashboardItem{
			endpoint("ep:http:get", dashboard.CategoryHTTP, "get",
				[]dashboard.SignalReference{
					metric("m_count", `http_requests_total";alert(1)`, "counter", "service", "operation", "status"),
					metric("m_ok", "http_requests_total", "counter", "service", "operation", "status"),
				},
				[]dashboard.SignalReference{span("s1", `GET /users";drop`)})},
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	hadSensitive := false
	canaries := []string{`alert(1)`, `etc?x=`, `";drop`}
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped {
			hadSensitive = true
		}
		for _, canary := range canaries {
			if strings.Contains(diagnostic.Message, canary) || strings.Contains(diagnostic.Field, canary) {
				t.Errorf("diagnostic leaks canary %q: %+v", canary, diagnostic)
			}
		}
	}
	if !hadSensitive {
		t.Errorf("expected sensitive-value diagnostics, got %v", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rendered rows = %d, want 1", len(rows))
	}
	for _, row := range rows {
		for _, panel := range row.Panels {
			for _, canary := range canaries {
				if strings.Contains(panel.Title, canary) {
					t.Errorf("title leaks canary %q: %q", canary, panel.Title)
				}
			}
			switch panel.Title {
			case "HTTP get request rate", "HTTP get error ratio", "HTTP operations":
				// Titles derive only from the normalized operation.
			default:
				t.Errorf("unexpected panel title %q", panel.Title)
			}
			for _, target := range panel.Targets {
				for _, canary := range canaries {
					if strings.Contains(target.Expr, canary) {
						t.Errorf("query leaks canary %q: %s", canary, target.Expr)
					}
				}
			}
			for _, link := range panel.Links {
				for _, canary := range canaries {
					if strings.Contains(link.URL, canary) {
						t.Errorf("link leaks canary %q: %s", canary, link.URL)
					}
				}
			}
		}
	}
}

// TestPanelLimit fails with DASHBOARD_PANEL_LIMIT_EXCEEDED instead of
// truncating when a row exceeds 60 panels.
func TestPanelLimit(t *testing.T) {
	items := make([]dashboard.DashboardItem, 0, 11)
	for index := range 11 {
		items = append(items, fullHTTP("ep:http:"+pad3(index), "op"+pad3(index)))
	}
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items:         items,
	}
	plan, err := Build(catalog, resolvePolicy(t))
	if err == nil {
		t.Fatalf("Build must fail over the panel ceiling, got %d panels", totalPanels(plan))
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodePanelLimitExceeded {
		t.Fatalf("error = %v, want DASHBOARD_PANEL_LIMIT_EXCEEDED", err)
	}
}

// TestPermutationDeterminism reruns Build and Render on a permuted
// catalog and requires identical rendered bytes and diagnostics.
func TestPermutationDeterminism(t *testing.T) {
	first := fullCatalog()
	second := fullCatalog()
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
	firstRows, err := Render(firstPlan)
	if err != nil {
		t.Fatalf("Render(first): %v", err)
	}
	secondRows, err := Render(secondPlan)
	if err != nil {
		t.Fatalf("Render(second): %v", err)
	}
	if !reflect.DeepEqual(firstRows, secondRows) {
		t.Fatal("rendered rows differ after permutation")
	}
}

func totalPanels(plan *Plan) int {
	total := 0
	for _, row := range plan.Rows {
		total += len(row.Panels)
	}
	return total
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

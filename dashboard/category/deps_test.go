package category

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

func depsItem(id, dependencyKind string, category dashboard.Category, operation string, metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.DashboardItem {
	item := dashboard.DashboardItem{
		ID: id, Category: category, DependencyKind: dependencyKind,
		Target:    dashboard.TargetRef{Kind: "dependency", ID: id},
		Operation: operation,
		Metrics:   metrics,
		Spans:     spans,
	}
	item.Capabilities = capabilitiesOf(metrics, spans)
	return item
}

func fullSQL(id, operation string) dashboard.DashboardItem {
	return depsItem(id, "sql", dashboard.CategoryDatabase, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "sql_queries_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "sql_query_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Query/"+operation)})
}

func fullRedis(id, operation string) dashboard.DashboardItem {
	return depsItem(id, "redis", dashboard.CategoryCache, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "redis_commands_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "redis_command_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Get/"+operation)})
}

func fullHTTPClient(id, operation string) dashboard.DashboardItem {
	return depsItem(id, "http_client", dashboard.CategoryHTTP, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "client_http_requests_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "client_http_request_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "HTTP "+operation)})
}

func fullRPCClient(id, operation string) dashboard.DashboardItem {
	return depsItem(id, "rpc_client", dashboard.CategoryRPC, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "client_rpc_requests_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "client_rpc_request_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Invoke/"+operation)})
}

func depsCatalog() *dashboard.DashboardCatalog {
	return &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			fullSQL("dep:sql:orders", "query"),
			fullSQL("dep:sql:users", "select"),
			fullRedis("dep:redis:session", "get"),
			fullRedis("dep:redis:cache", "set"),
			fullHTTPClient("dep:http:gateway", "call"),
			fullRPCClient("dep:rpc:ledger", "invoke"),
		},
	}
}

// TestBuildDepsFull is AC1: SQL and Redis (and the client subsections)
// generate rate, error ratio, duration percentiles, operation breakdown
// and CLIENT-span trace links; queries reference only plan metrics and
// labels, never raw SQL/Redis/URL values.
func TestBuildDepsFull(t *testing.T) {
	plan, err := BuildDependencies(depsCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	if len(plan.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 (database, cache, http client, rpc client)", len(plan.Rows))
	}
	wantTitles := []string{"Database", "Cache", "HTTP Client Calls", "RPC Client Calls"}
	wantCategories := []dashboard.Category{
		dashboard.CategoryDatabase, dashboard.CategoryCache,
		dashboard.CategoryHTTP, dashboard.CategoryRPC,
	}
	// Database/Cache rows aggregate two items; each client row owns one.
	wantTableTargets := []int{2, 2, 1, 1}
	for index, row := range plan.Rows {
		if row.Title != wantTitles[index] {
			t.Errorf("row %d title = %q, want %q", index, row.Title, wantTitles[index])
		}
		if row.Category != wantCategories[index] {
			t.Errorf("row %d category = %s, want %s", index, row.Category, wantCategories[index])
		}
		if len(row.Panels) != 6 {
			t.Errorf("row %d panels = %d, want 6 (rate, error, p50, p95, p99, operations)", index, len(row.Panels))
		}
		if row.Description == "" {
			t.Errorf("row %d description is empty", index)
		}
		ratePanels, tableTargets := 0, 0
		for _, panel := range row.Panels {
			if panel.Purpose == "rate" {
				ratePanels++
				if len(panel.Links) == 0 {
					t.Errorf("row %d rate panel lacks trace link", index)
				}
			}
			if panel.Purpose == "operations" {
				tableTargets = len(panel.Targets)
				if len(panel.Targets) != wantTableTargets[index] {
					t.Errorf("row %d operation table targets = %d, want %d", index, len(panel.Targets), wantTableTargets[index])
				}
			}
		}
		if ratePanels != 1 {
			t.Errorf("row %d rate panels = %d, want 1", index, ratePanels)
		}
		if tableTargets != wantTableTargets[index] {
			t.Errorf("row %d table targets = %d, want %d", index, tableTargets, wantTableTargets[index])
		}
	}

	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("rendered rows = %d, want 4", len(rows))
	}
	validateRenderedDashboard(t, rows)
}

// TestBuildDepsClientToggle is AC2: with
// policy.IncludeClientDependencies=false no client rows are planned and
// the Database/Cache rows are unaffected; with it true the client
// subsections appear with distinct stable titles.
func TestBuildDepsClientToggle(t *testing.T) {
	clients := false
	policy, err := dashboard.Resolve(&dashboard.DashboardConfig{
		IncludeClientDependencies: &clients,
	}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	plan, err := BuildDependencies(depsCatalog(), *policy)
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (database, cache)", len(plan.Rows))
	}
	for _, row := range plan.Rows {
		if strings.Contains(row.Title, "Client") {
			t.Errorf("client row planned despite include_client_dependencies=false: %q", row.Title)
		}
		if len(row.Panels) == 0 {
			t.Errorf("non-client row %q has no panels", row.Title)
		}
	}

	onPlan, err := BuildDependencies(depsCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies(true) failed: %v", err)
	}
	if len(onPlan.Rows) != 4 {
		t.Fatalf("rows with clients enabled = %d, want 4", len(onPlan.Rows))
	}
	for _, row := range onPlan.Rows {
		for _, panel := range row.Panels {
			for _, target := range panel.Targets {
				if strings.Contains(target.Expr, "sql_queries_total") && strings.Contains(row.Title, "Client") {
					t.Errorf("client row %q leaks a server/database metric: %s", row.Title, target.Expr)
				}
			}
		}
	}
}

// TestBuildDepsClientSeparation is AC2: client rows use client-only
// metric families and titles that never collide with the P2-07 server
// rows; the rendered client rows carry distinct row IDs.
func TestBuildDepsClientSeparation(t *testing.T) {
	plan, err := BuildDependencies(depsCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	clientRowIDs := make(map[int]bool)
	for _, rowPlan := range plan.Rows {
		if rowPlan.Category == dashboard.CategoryHTTP || rowPlan.Category == dashboard.CategoryRPC {
			for _, panel := range rowPlan.Panels {
				for _, target := range panel.Targets {
					if !strings.Contains(target.Expr, "client_") {
						t.Errorf("client row %q mixes a server metric into its query: %s", rowPlan.Title, target.Expr)
					}
				}
			}
		}
	}
	for _, row := range rows {
		if strings.Contains(row.Title, "Client") {
			clientRowIDs[row.ID] = true
		}
	}
	if len(clientRowIDs) != 2 {
		t.Errorf("client row IDs = %v, want 2 distinct IDs", clientRowIDs)
	}
}

// TestBuildDepsDegradation is AC4: counter-only dependencies generate only
// provable rate/error/operation panels; percentile panels are omitted with
// diagnostics.
func TestBuildDepsDegradation(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			depsItem("dep:sql:orders", "sql", dashboard.CategoryDatabase, "query",
				[]dashboard.SignalReference{
					metric("m_sql", "sql_queries_total", "counter", "service", "operation", "status"),
				}, nil),
			depsItem("dep:redis:session", "redis", dashboard.CategoryCache, "get",
				[]dashboard.SignalReference{
					metric("m_redis", "redis_commands_total", "counter", "service", "operation", "status"),
				}, nil),
		},
	}
	plan, err := BuildDependencies(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	for _, row := range plan.Rows {
		if row.Category != dashboard.CategoryDatabase && row.Category != dashboard.CategoryCache {
			continue
		}
		for _, panel := range row.Panels {
			if panel.Purpose == "p50" || panel.Purpose == "p95" || panel.Purpose == "p99" {
				t.Errorf("percentile panel generated without histogram: %s", panel.Purpose)
			}
		}
	}
	foundPercentile := false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeMissingRequiredMetric && strings.Contains(diagnostic.Field, ".p50") {
			foundPercentile = true
		}
	}
	if !foundPercentile {
		t.Errorf("expected percentile degradation diagnostics, got %v", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	validateRenderedDashboard(t, rows)
}

// TestBuildDepsNoEntities is AC3: with no dependency entities every row is
// omitted and DASHBOARD_EMPTY_CATEGORY plus the catalog's unsupported
// diagnostics are preserved.
func TestBuildDepsNoEntities(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Diagnostics: []dashboard.Diagnostic{{
			Code: dashboard.CodeUnsupportedTarget, TargetID: "dep:dynamic:sql",
			Field:   "dependencies[dep:dynamic:sql].kind",
			Message: "dependency kind has no safe v1 dashboard mapping",
		}},
	}
	plan, err := BuildDependencies(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	if len(plan.Rows) != 4 {
		t.Fatalf("rows = %d, want 4 empty row plans", len(plan.Rows))
	}
	foundEmpty, foundUnsupported := 0, false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeEmptyCategory {
			foundEmpty++
		}
		if diagnostic.Code == dashboard.CodeUnsupportedTarget {
			foundUnsupported = true
		}
	}
	if foundEmpty != 4 {
		t.Errorf("empty-category diagnostics = %d, want 4", foundEmpty)
	}
	if !foundUnsupported {
		t.Errorf("catalog unsupported-target diagnostic not preserved: %v", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty rows must be omitted, got %d rows", len(rows))
	}
}

// TestDepsCanary is AC1/AC3: raw SQL text, Redis keys/values, URL
// userinfo/query, Authorization headers, RPC targets and PII never reach
// queries, links, titles, diagnostics or errors.
func TestDepsCanary(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			depsItem("dep:sql:inj", "sql", dashboard.CategoryDatabase,
				`select * from users where email='a@b.c'`,
				[]dashboard.SignalReference{
					metric("m_bad", `sql_queries_total";alert(1)`, "counter", "service", "operation", "status"),
					metric("m_ok", "sql_queries_total", "counter", "service", "operation", "status"),
				},
				[]dashboard.SignalReference{span("s_bad", `Query";drop`)}),
			depsItem("dep:redis:key", "redis", dashboard.CategoryCache, `get session:token:abc`,
				[]dashboard.SignalReference{
					metric("m_key", "redis_commands_total", "counter", "service", "operation", "status"),
					metric("m_h", "redis_command_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s_key", "Get/session")}),
			depsItem("dep:http:auth", "http_client", dashboard.CategoryHTTP,
				`Authorization: Bearer token123`,
				[]dashboard.SignalReference{
					metric("m_http", "client_http_requests_total", "counter", "service", "operation", "status"),
				}, nil),
			depsItem("dep:rpc:tgt", "rpc_client", dashboard.CategoryRPC,
				`target=10.0.0.1:9090 user=admin`,
				[]dashboard.SignalReference{
					metric("m_rpc", "client_rpc_requests_total", "counter", "service", "operation", "status"),
				}, nil),
		},
	}
	plan, err := BuildDependencies(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	canaries := []string{
		`alert(1)`, `";drop`, `select * from users`, `a@b.c`,
		`session:token`, `Bearer`, `token123`, `10.0.0.1`, `admin`, `:9090`,
	}
	for _, diagnostic := range plan.Diagnostics {
		for _, canary := range canaries {
			if strings.Contains(diagnostic.Message, canary) || strings.Contains(diagnostic.Field, canary) {
				t.Errorf("diagnostic leaks canary %q: %+v", canary, diagnostic)
			}
		}
	}
	foundMetricDrop, foundSpanDrop, foundOperationDrop := false, false, false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped && diagnostic.Field == "metrics[].name" {
			foundMetricDrop = true
		}
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped && diagnostic.Field == "spans[].name" {
			foundSpanDrop = true
		}
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped && diagnostic.Field == "operation_breakdown[].operation" {
			foundOperationDrop = true
		}
	}
	if !foundMetricDrop || !foundSpanDrop || !foundOperationDrop {
		t.Errorf("expected metric/span/operation sensitive-value diagnostics, got %v", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	rendered, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	for _, canary := range canaries {
		if strings.Contains(string(rendered), canary) {
			t.Errorf("rendered dependency output leaks %q", canary)
		}
	}
	for _, row := range rows {
		for _, panel := range row.Panels {
			for _, link := range panel.Links {
				if strings.Contains(link.URL, "var-operation=&") {
					t.Errorf("trace link carries an empty operation: %s", link.URL)
				}
			}
		}
	}
}

// TestDepsPermutation requires identical rendered rows and diagnostics
// regardless of input order.
func TestDepsPermutation(t *testing.T) {
	first := depsCatalog()
	second := depsCatalog()
	for left, right := 0, len(second.Items)-1; left < right; left, right = left+1, right-1 {
		second.Items[left], second.Items[right] = second.Items[right], second.Items[left]
	}
	firstPlan, err := BuildDependencies(first, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies(first): %v", err)
	}
	secondPlan, err := BuildDependencies(second, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies(second): %v", err)
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
		t.Fatal("rendered dependency rows differ after permutation")
	}
}

// TestDepsQueryLimit fails with DASHBOARD_PANEL_LIMIT_EXCEEDED instead of
// truncating when one dependency row exceeds 150 queries. Each unique
// metric family contributes rate+error+3 percentiles+1 table target, so
// 26 SQL items exceed the ceiling while panel count stays under 60.
func TestDepsQueryLimit(t *testing.T) {
	items := make([]dashboard.DashboardItem, 0, 26)
	for index := range 26 {
		items = append(items, depsItem("dep:sql:"+pad3(index), "sql", dashboard.CategoryDatabase, "op"+pad3(index),
			[]dashboard.SignalReference{
				metric("m"+pad3(index), "sql_queries_"+pad3(index)+"_total", "counter", "service", "operation", "status"),
				metric("h"+pad3(index), "sql_query_duration_"+pad3(index), "histogram", "service", "operation"),
			}, nil))
	}
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items:         items,
	}
	plan, err := BuildDependencies(catalog, resolvePolicy(t))
	if err == nil {
		t.Fatalf("BuildDependencies must fail over the query ceiling, got %d queries", totalQueries(plan))
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodePanelLimitExceeded {
		t.Fatalf("error = %v, want DASHBOARD_PANEL_LIMIT_EXCEEDED", err)
	}
}

// TestDepsModelValidation runs the rendered rows through the P2-02 model
// validator and renderer.
func TestDepsModelValidation(t *testing.T) {
	plan, err := BuildDependencies(depsCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildDependencies failed: %v", err)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	validateRenderedDashboard(t, rows)
}

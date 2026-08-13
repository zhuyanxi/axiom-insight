package pipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

func metric(planID, name, metricType string, attributes ...string) dashboard.SignalReference {
	return dashboard.SignalReference{
		PlanID: planID, Name: name, Type: metricType, Attributes: attributes,
	}
}

func span(planID, name string) dashboard.SignalReference {
	return dashboard.SignalReference{PlanID: planID, Name: name, Type: "client"}
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

func endpoint(id string, category dashboard.Category, operation string, metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.DashboardItem {
	item := dashboard.DashboardItem{
		ID: id, Category: category, Target: dashboard.TargetRef{Kind: "endpoint", ID: id},
		Operation: operation, Metrics: metrics, Spans: spans,
	}
	item.Capabilities = capabilitiesOf(metrics, spans)
	return item
}

func dependency(id, dependencyKind string, category dashboard.Category, operation string, metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.DashboardItem {
	item := dashboard.DashboardItem{
		ID: id, Category: category, DependencyKind: dependencyKind,
		Target:    dashboard.TargetRef{Kind: "dependency", ID: id},
		Operation: operation, Metrics: metrics, Spans: spans,
	}
	item.Capabilities = capabilitiesOf(metrics, spans)
	return item
}

// fullCatalog is the P2-10 composite fixture: HTTP/RPC endpoints, Kafka,
// SQL, Redis and client dependencies, each at full capability. Endpoint
// rows share the request-duration histogram and in-flight gauge families;
// dependency rows share one dependency-duration histogram family, so the
// P2-06 overview stays under its 30-query ceiling (7 counter + 2
// histogram + 1 gauge families = 28 overview queries) while every
// category still unlocks rate, error ratio, percentiles, in-flight and
// trace links.
func fullCatalog() *dashboard.DashboardCatalog {
	return &dashboard.DashboardCatalog{
		SchemaVersion:               dashboard.CatalogSchemaVersion,
		SourceIRSchemaVersion:       "v1",
		GenerationPlanSchemaVersion: "generation_plan/v1",
		ServiceName:                 "payment",
		Items: []dashboard.DashboardItem{
			endpoint("ep:http:get", dashboard.CategoryHTTP, "get",
				[]dashboard.SignalReference{
					metric("m:http:count", "http_requests_total", "counter", "service", "operation", "status"),
					metric("m:http:dur", "request_duration", "histogram", "service", "operation"),
					metric("m:http:inf", "in_flight", "gauge", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:http", "GET /get")}),
			endpoint("ep:rpc:charge", dashboard.CategoryRPC, "charge",
				[]dashboard.SignalReference{
					metric("m:rpc:count", "rpc_requests_total", "counter", "service", "operation", "status"),
					metric("m:rpc:dur", "request_duration", "histogram", "service", "operation"),
					metric("m:rpc:inf", "in_flight", "gauge", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:rpc", "Charge/charge")}),
			dependency("dep:kafka:orders", "kafka_producer", dashboard.CategoryKafka, "produce",
				[]dashboard.SignalReference{
					metric("m:kafka:count", "kafka_messages_total", "counter", "service", "operation", "status"),
					metric("m:kafka:dur", "dependency_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:kafka", "Publish/produce")}),
			dependency("dep:sql:orders", "sql", dashboard.CategoryDatabase, "query",
				[]dashboard.SignalReference{
					metric("m:sql:count", "sql_queries_total", "counter", "service", "operation", "status"),
					metric("m:sql:dur", "dependency_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:sql", "Query/query")}),
			dependency("dep:redis:session", "redis", dashboard.CategoryCache, "get",
				[]dashboard.SignalReference{
					metric("m:redis:count", "redis_commands_total", "counter", "service", "operation", "status"),
					metric("m:redis:dur", "dependency_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:redis", "Get/get")}),
			dependency("dep:http:gateway", "http_client", dashboard.CategoryHTTP, "call",
				[]dashboard.SignalReference{
					metric("m:client:http", "client_http_requests_total", "counter", "service", "operation", "status"),
					metric("m:client:http:dur", "dependency_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:client:http", "HTTP call")}),
			dependency("dep:rpc:ledger", "rpc_client", dashboard.CategoryRPC, "invoke",
				[]dashboard.SignalReference{
					metric("m:client:rpc", "client_rpc_requests_total", "counter", "service", "operation", "status"),
					metric("m:client:rpc:dur", "dependency_duration", "histogram", "service", "operation"),
				},
				[]dashboard.SignalReference{span("s:client:rpc", "Invoke/invoke")}),
		},
	}
}

func resolvePolicy(t *testing.T) dashboard.DashboardPolicy {
	t.Helper()
	policy, err := dashboard.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	return *policy
}

// TestBuildFull is AC1: the composite catalog assembles into a plan with
// the canonical row order, the overview row first, controlled metadata
// and no diagnostics.
func TestBuildFull(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if plan.Title() != "payment Observability" {
		t.Errorf("title = %q, want %q", plan.Title(), "payment Observability")
	}
	if !strings.HasPrefix(plan.UID(), "si-") || !strings.HasSuffix(plan.UID(), "-v1") {
		t.Errorf("uid = %q, want si-<service>-v1 shape", plan.UID())
	}
	if plan.PolicyDigest() == "" {
		t.Error("policy digest is empty")
	}
	if plan.Timezone() != "browser" || plan.Refresh() != "30s" {
		t.Errorf("timezone/refresh = %q/%q, want browser/30s", plan.Timezone(), plan.Refresh())
	}

	rows := plan.Rows()
	if len(rows) == 0 {
		t.Fatal("no rows planned")
	}
	if rows[0].Title != "Service Overview" {
		t.Errorf("first row = %q, want Service Overview", rows[0].Title)
	}
	wantTitles := []string{
		"Service Overview", "HTTP", "RPC", "Kafka", "Database", "Cache",
		"HTTP Client Calls", "RPC Client Calls",
	}
	if len(rows) != len(wantTitles) {
		t.Fatalf("rows = %d, want %d (%v)", len(rows), len(wantTitles), wantTitles)
	}
	for index, want := range wantTitles {
		if rows[index].Title != want {
			t.Errorf("row %d title = %q, want %q", index, rows[index].Title, want)
		}
	}

	rowIDs := make(map[int]bool)
	panelIDs := make(map[int]bool)
	for _, row := range rows {
		if rowIDs[row.ID] {
			t.Errorf("duplicate row ID %d", row.ID)
		}
		rowIDs[row.ID] = true
		for _, panel := range row.Panels {
			if panelIDs[panel.ID] {
				t.Errorf("duplicate panel ID %d", panel.ID)
			}
			panelIDs[panel.ID] = true
		}
	}

	variables := plan.Variables()
	foundDatasource, foundOperation := false, false
	for _, variable := range variables {
		if variable.Name == "datasource" {
			foundDatasource = true
		}
		if variable.Name == "operation" {
			foundOperation = true
		}
	}
	if !foundDatasource || !foundOperation {
		t.Errorf("variables = %+v, want datasource and operation", variables)
	}
	if len(plan.Diagnostics()) != 0 {
		t.Errorf("diagnostics = %v, want none for the full catalog", plan.Diagnostics())
	}
}

// TestBuildEmpty fails with DASHBOARD_EMPTY_CATEGORY when no panel can be
// generated.
func TestBuildEmpty(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
	}
	_, err := Build(catalog, resolvePolicy(t))
	if err == nil {
		t.Fatal("Build must fail for an empty catalog")
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodeEmptyCategory {
		t.Fatalf("error = %v, want DASHBOARD_EMPTY_CATEGORY", err)
	}
}

// TestBuildLimits fails with DASHBOARD_PANEL_LIMIT_EXCEEDED when the
// assembled dashboard exceeds the policy ceilings.
func TestBuildLimits(t *testing.T) {
	panels := int64(2)
	panelPolicy, err := dashboard.Resolve(&dashboard.DashboardConfig{MaxPanels: &panels}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	_, err = Build(fullCatalog(), *panelPolicy)
	assertPanelLimitError(t, err)

	queries := int64(5)
	queryPolicy, err := dashboard.Resolve(&dashboard.DashboardConfig{MaxQueries: &queries}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	_, err = Build(fullCatalog(), *queryPolicy)
	assertPanelLimitError(t, err)
}

func assertPanelLimitError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Build must fail over the policy ceiling")
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodePanelLimitExceeded {
		t.Fatalf("error = %v, want DASHBOARD_PANEL_LIMIT_EXCEEDED", err)
	}
}

// TestBuildDeterminism is AC3: identical and permuted catalogs produce
// semantically identical plans.
func TestBuildDeterminism(t *testing.T) {
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
	if firstPlan.Title() != secondPlan.Title() || firstPlan.UID() != secondPlan.UID() {
		t.Errorf("metadata differs after permutation")
	}
	if !reflect.DeepEqual(firstPlan.Variables(), secondPlan.Variables()) {
		t.Errorf("variables differ after permutation")
	}
	if !reflect.DeepEqual(firstPlan.Rows(), secondPlan.Rows()) {
		t.Error("rows differ after permutation")
	}
	if !reflect.DeepEqual(firstPlan.Diagnostics(), secondPlan.Diagnostics()) {
		t.Errorf("diagnostics differ after permutation")
	}
}

// TestBuildGridStacking verifies the rows stack top-to-bottom with no
// overlaps: every panel stays on the 24-column grid, and every row sits
// entirely below the previous row.
func TestBuildGridStacking(t *testing.T) {
	plan, err := Build(fullCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	rows := plan.Rows()
	previousBottom := 0
	for rowIndex, row := range rows {
		rowMinY, rowMaxBottom := -1, -1
		for _, panel := range row.Panels {
			grid := panel.GridPos
			if grid.X < 0 || grid.W < 1 || grid.W > dashboard.GridColumns || grid.X+grid.W > dashboard.GridColumns {
				t.Errorf("row %d panel %d grid out of bounds: %+v", rowIndex, panel.ID, grid)
			}
			if grid.Y < 0 {
				t.Errorf("row %d panel %d has negative Y: %+v", rowIndex, panel.ID, grid)
			}
			if rowMinY == -1 || grid.Y < rowMinY {
				rowMinY = grid.Y
			}
			if bottom := grid.Y + grid.H; bottom > rowMaxBottom {
				rowMaxBottom = bottom
			}
		}
		if len(row.Panels) > 0 {
			if rowMinY < previousBottom {
				t.Errorf("row %d overlaps the previous row: min y=%d, previous bottom=%d", rowIndex, rowMinY, previousBottom)
			}
			previousBottom = rowMaxBottom
		}
	}
}

// TestBuildClientToggle is AC2: disabling include_client_dependencies
// removes the client subsections and leaves the server rows intact.
func TestBuildClientToggle(t *testing.T) {
	clients := false
	policy, err := dashboard.Resolve(&dashboard.DashboardConfig{
		IncludeClientDependencies: &clients,
	}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	plan, err := Build(fullCatalog(), *policy)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	for _, row := range plan.Rows() {
		if strings.Contains(row.Title, "Client") {
			t.Errorf("client row planned despite include_client_dependencies=false: %q", row.Title)
		}
	}
	if len(plan.Rows()) != 6 {
		t.Errorf("rows = %d, want 6 without client subsections", len(plan.Rows()))
	}
}

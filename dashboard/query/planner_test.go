package query

import (
	"slices"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// fullItem is the composite fixture: counter with status, histogram,
// gauge and a span, every capability available.
func fullItem() *dashboard.DashboardItem {
	return &dashboard.DashboardItem{
		ID:         "item:http:get_user",
		Category:   dashboard.CategoryHTTP,
		Operation:  "get",
		Provenance: []string{"endpoints[get_user]"},
		Metrics: []dashboard.SignalReference{
			{PlanID: "m_counter", Name: "http_requests_total", Type: "counter", Attributes: []string{"operation", "service", "status"}},
			{PlanID: "m_hist", Name: "http_request_duration", Type: "histogram", Attributes: []string{"operation", "service"}},
			{PlanID: "m_gauge", Name: "http_in_flight", Type: "gauge", Attributes: []string{"operation", "service"}},
		},
		Spans: []dashboard.SignalReference{{PlanID: "s_span", Name: "GET /users", Type: "server"}},
		Capabilities: dashboard.Capabilities{
			Rate:        dashboard.QueryCapability{Available: true},
			ErrorRatio:  dashboard.QueryCapability{Available: true},
			Percentiles: dashboard.QueryCapability{Available: true},
			InFlight:    dashboard.QueryCapability{Available: true},
			TraceLink:   dashboard.QueryCapability{Available: true},
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

// TestPlanItemQueriesMatrix covers every metric type and capability
// combination (table-driven query matrix): Counter/Histogram/Gauge,
// missing attributes and unavailable capabilities.
func TestPlanItemQueriesMatrix(t *testing.T) {
	cases := []struct {
		name       string
		item       *dashboard.DashboardItem
		wantKinds  map[QueryKind]int
		wantDiag   string
		wantNoDiag string
	}{
		{
			name: "full item",
			item: fullItem(),
			wantKinds: map[QueryKind]int{
				QueryKindRate: 1, QueryKindErrorRatio: 1,
				QueryKindPercentile: 3, QueryKindInFlight: 1,
			},
		},
		{
			name: "counter only, no status",
			item: itemWithMetrics(
				signal("m_counter", "requests_total", "counter", "service", "operation")),
			wantKinds: map[QueryKind]int{QueryKindRate: 1},
			wantDiag:  "capabilities.error_ratio",
		},
		{
			name: "counter only, no service operation",
			item: itemWithMetrics(
				signal("m_counter", "requests_total", "counter", "status")),
			wantKinds: map[QueryKind]int{QueryKindErrorRatio: 1},
			wantDiag:  "capabilities.rate",
		},
		{
			name: "histogram only",
			item: itemWithMetrics(
				signal("m_hist", "request_duration", "histogram", "service", "operation")),
			wantKinds: map[QueryKind]int{QueryKindPercentile: 3},
		},
		{
			name: "histogram without service operation",
			item: itemWithMetrics(
				signal("m_hist", "request_duration", "histogram")),
			wantKinds: map[QueryKind]int{},
			wantDiag:  "metrics[].type",
		},
		{
			name: "gauge only",
			item: itemWithMetrics(
				signal("m_gauge", "in_flight", "gauge", "service", "operation")),
			wantKinds: map[QueryKind]int{QueryKindInFlight: 1},
		},
		{
			name:      "no metrics at all",
			item:      itemWithMetrics(),
			wantKinds: map[QueryKind]int{},
			wantDiag:  "capabilities.rate",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plans, links, diagnostics := PlanItemQueries(test.item, "payment", resolvePolicy(t))
			if len(test.item.Spans) == 0 && len(links) != 0 {
				t.Errorf("no span plans: expected 0 trace links, got %d", len(links))
			}
			counts := make(map[QueryKind]int)
			for _, plan := range plans {
				counts[plan.Kind]++
				if plan.ItemID != test.item.ID {
					t.Errorf("plan ItemID = %q, want %q", plan.ItemID, test.item.ID)
				}
				if !strings.HasPrefix(plan.CanonicalKey, "query:") {
					t.Errorf("canonical key %q lacks query: prefix", plan.CanonicalKey)
				}
			}
			for kind, want := range test.wantKinds {
				if counts[kind] != want {
					t.Errorf("kind %s count = %d, want %d", kind, counts[kind], want)
				}
			}
			diagText := strings.Join(diagnosticMessages(diagnostics), " ")
			if test.wantDiag != "" && !strings.Contains(diagText, test.wantDiag) {
				t.Errorf("expected diagnostic field %q, got %q", test.wantDiag, diagText)
			}
			if test.wantNoDiag != "" && strings.Contains(diagText, test.wantNoDiag) {
				t.Errorf("unexpected diagnostic field %q in %q", test.wantNoDiag, diagText)
			}
		})
	}
}

func signal(planID, name, metricType string, attributes ...string) dashboard.SignalReference {
	return dashboard.SignalReference{PlanID: planID, Name: name, Type: metricType, Attributes: attributes}
}

func itemWithMetrics(metrics ...dashboard.SignalReference) *dashboard.DashboardItem {
	item := &dashboard.DashboardItem{
		ID: "item:http:handler", Category: dashboard.CategoryHTTP, Operation: "get",
		Metrics: metrics,
	}
	// Recompute capabilities exactly like the catalog builder: rate needs
	// service+operation, error ratio needs status, percentiles need a
	// histogram, in-flight needs a gauge.
	var counters, histograms, gauges int
	hasStatus, hasServiceOperation := false, false
	for _, metric := range metrics {
		switch metric.Type {
		case "counter":
			counters++
		case "histogram":
			histograms++
		case "gauge":
			gauges++
		}
		if contains(metric.Attributes, "status") {
			hasStatus = true
		}
		if contains(metric.Attributes, "service") && contains(metric.Attributes, "operation") {
			hasServiceOperation = true
		}
	}
	item.Capabilities = dashboard.Capabilities{
		Rate:        dashboard.QueryCapability{Available: counters > 0 && hasServiceOperation},
		ErrorRatio:  dashboard.QueryCapability{Available: counters > 0 && hasStatus},
		Percentiles: dashboard.QueryCapability{Available: histograms > 0},
		InFlight:    dashboard.QueryCapability{Available: gauges > 0},
	}
	return item
}

func contains(values []string, wanted string) bool {
	return slices.Contains(values, wanted)
}

func diagnosticMessages(diagnostics []dashboard.Diagnostic) []string {
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		messages = append(messages, diagnostic.Field)
	}
	return messages
}

// TestPlanItemQueriesDeterministic reruns the same item and compares the
// rendered query bytes and diagnostics.
func TestPlanItemQueriesDeterministic(t *testing.T) {
	item := fullItem()
	policy := resolvePolicy(t)
	firstPlans, firstLinks, firstDiags := PlanItemQueries(item, "payment", policy)
	firstText := renderAll(t, firstPlans)
	for round := range 10 {
		plans, links, diags := PlanItemQueries(item, "payment", policy)
		if renderAll(t, plans) != firstText {
			t.Fatalf("round %d renders differently", round)
		}
		if len(links) != len(firstLinks) || len(diags) != len(firstDiags) {
			t.Fatalf("round %d link/diagnostic counts differ", round)
		}
	}
}

func renderAll(t *testing.T, plans []QueryPlan) string {
	t.Helper()
	var builder strings.Builder
	for _, plan := range plans {
		text, err := Render(plan.Expression)
		if err != nil {
			t.Fatalf("render %s: %v", plan.CanonicalKey, err)
		}
		builder.WriteString(plan.CanonicalKey)
		builder.WriteString("=")
		builder.WriteString(text)
		builder.WriteString("\n")
	}
	return builder.String()
}

// TestPercentileOrder pins the fixed quantile order and purposes.
func TestPercentileOrder(t *testing.T) {
	item := fullItem()
	plans, _, _ := PlanItemQueries(item, "payment", resolvePolicy(t))
	var purposes []string
	for _, plan := range plans {
		if plan.Kind == QueryKindPercentile {
			purposes = append(purposes, plan.Purpose)
			if len(plan.Metadata.Quantiles) != 1 || plan.Metadata.Quantiles[0] != planExpressionQuantile(t, plan) {
				t.Errorf("metadata quantile mismatch for %s", plan.Purpose)
			}
		}
	}
	want := []string{"p50", "p95", "p99"}
	if len(purposes) != 3 {
		t.Fatalf("percentile purposes = %v", purposes)
	}
	for index := range want {
		if purposes[index] != want[index] {
			t.Errorf("quantile order = %v, want %v", purposes, want)
		}
	}
}

func planExpressionQuantile(t *testing.T, plan QueryPlan) float64 {
	t.Helper()
	expression, ok := plan.Expression.(*HistogramQuantileExpression)
	if !ok {
		t.Fatalf("percentile plan expression is %T", plan.Expression)
	}
	return expression.Quantile
}

// TestTraceLinkGates covers the trace-link policy gate: disabled by
// policy, missing spans and canary span names.
func TestTraceLinkGates(t *testing.T) {
	t.Run("policy disables links", func(t *testing.T) {
		policy := resolvePolicy(t)
		policy.IncludeTraceLinks = false
		_, links, diagnostics := PlanItemQueries(fullItem(), "payment", policy)
		if len(links) != 0 {
			t.Errorf("links generated with include_trace_links=false")
		}
		if len(diagnostics) != 0 {
			t.Errorf("no diagnostics expected when links are disabled, got %d", len(diagnostics))
		}
	})

	t.Run("no span plans", func(t *testing.T) {
		item := fullItem()
		item.Spans = nil
		_, links, diagnostics := PlanItemQueries(item, "payment", resolvePolicy(t))
		if len(links) != 0 {
			t.Errorf("links generated without span plans")
		}
		found := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == dashboard.CodeMissingRequiredMetric && strings.Contains(diagnostic.Field, "trace_link") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected trace_link capability diagnostic, got %v", diagnostics)
		}
	})

	t.Run("canary span name dropped", func(t *testing.T) {
		item := fullItem()
		item.Spans[0].Name = "GET /users\"onload=alert(1)\x00"
		_, links, diagnostics := PlanItemQueries(item, "payment", resolvePolicy(t))
		if len(links) != 0 {
			t.Errorf("link generated with canary span name")
		}
		found := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == dashboard.CodeSensitiveValueDropped {
				found = true
			}
		}
		if !found {
			t.Errorf("expected DASHBOARD_SENSITIVE_VALUE_DROPPED, got %v", diagnostics)
		}
	})
}

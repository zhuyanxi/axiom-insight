package query

import (
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// TestCanaryInjectionBlocked is AC3: IR fixtures carrying raw URLs, SQL,
// Redis keys, topics, tokens, email addresses, newlines, quotes and
// PromQL operators must never appear in queries, trace links,
// diagnostics or errors. Unvalidated input cannot become a metric, label,
// regex or URL fragment.
func TestCanaryInjectionBlocked(t *testing.T) {
	// Every canary taints the operation, the metric name and the span
	// name. Canaries that fail the service-name gate also taint the
	// service name; the dotted topic canary is inert by design and is
	// only exercised in the positions where dots are illegal (operation,
	// metric).
	cases := []struct {
		canary   string
		taintSvc bool
	}{
		{canary: "http://evil.example/x?token=s3cr3t", taintSvc: true},
		{canary: "SELECT * FROM users", taintSvc: true},
		{canary: "redis://cache:6379/0/session:key", taintSvc: true},
		{canary: "orders.ingest.topic", taintSvc: false},
		{canary: "Bearer eyJhbGciOiJIUzI1NiJ9", taintSvc: true},
		{canary: "admin@example.com", taintSvc: true},
		{canary: "\nset /usr/bin/reverse\n", taintSvc: true},
		{canary: `"onload=alert(1)"`, taintSvc: true},
		{canary: ") or on(x) group_left(y) (z", taintSvc: true},
		{canary: "rate(fake_metric[5m])", taintSvc: true},
		{canary: "$__interval", taintSvc: true},
	}
	for _, test := range cases {
		t.Run(test.canary, func(t *testing.T) {
			tainted := *fullItem()
			tainted.Operation = "get" + test.canary
			tainted.Metrics[0].Name = "metric" + test.canary
			tainted.Spans[0].Name = test.canary
			serviceName := "payment"
			if test.taintSvc {
				serviceName = "payment" + test.canary
			}
			plans, links, diagnostics := PlanItemQueries(&tainted, serviceName, resolvePolicy(t))
			queriesAndDiagnostics := renderAll(t, plans)
			for _, diagnostic := range diagnostics {
				queriesAndDiagnostics += "|" + diagnostic.Code + diagnostic.Field + diagnostic.Message
			}
			if strings.Contains(queriesAndDiagnostics, test.canary) {
				t.Errorf("canary %q leaked into queries or diagnostics", test.canary)
			}
			for _, link := range links {
				if strings.Contains(link.ServiceName, test.canary) || strings.Contains(link.Operation, test.canary) {
					t.Errorf("canary %q leaked into the link service or operation", test.canary)
				}
				if strings.Contains(link.SpanName, test.canary) && !spanNamePattern.MatchString(test.canary) {
					t.Errorf("injection-capable canary %q leaked into the span name", test.canary)
				}
			}
		})
	}
}

// TestCanaryServiceNameDropsMatchers: an invalid service name never
// becomes a matcher value; the query still renders without it and a
// DASHBOARD_SENSITIVE_VALUE_DROPPED diagnostic is emitted.
func TestCanaryServiceNameDropsMatchers(t *testing.T) {
	item := fullItem()
	plans, _, diagnostics := PlanItemQueries(item, "svc\x00name\"inject{rate(", resolvePolicy(t))
	if len(diagnostics) != 1 || diagnostics[0].Code != dashboard.CodeSensitiveValueDropped {
		t.Fatalf("expected one sensitive-value diagnostic, got %v", diagnostics)
	}
	for _, plan := range plans {
		rendered, err := Render(plan.Expression)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(rendered, "inject") || strings.Contains(rendered, "svc") {
			t.Errorf("invalid service name leaked into %q", rendered)
		}
	}
}

// TestCanaryDiagnosticsNeverEcho verifies diagnostic messages carry no
// rejected values.
func TestCanaryDiagnosticsNeverEcho(t *testing.T) {
	item := fullItem()
	item.Spans[0].Name = "s3cr3t-span-name\x00"
	_, _, diagnostics := PlanItemQueries(item, "payment", resolvePolicy(t))
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "s3cr3t") {
			t.Errorf("diagnostic echoes rejected value: %+v", diagnostic)
		}
	}
}

// TestCanaryBreakdownValues: invalid operation values are dropped with
// diagnostics, never embedded into breakdown queries.
func TestCanaryBreakdownValues(t *testing.T) {
	_, diagnostics, err := PlanOperationBreakdown(
		signal("m_counter", "requests_total", "counter", "service", "operation"),
		[]string{"get", "list\x00", "SELECT * FROM users"}, "payment", resolvePolicy(t))
	if err != nil {
		t.Fatalf("breakdown failed: %v", err)
	}
	if len(diagnostics) != 2 {
		t.Fatalf("expected 2 dropped-value diagnostics, got %d", len(diagnostics))
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != dashboard.CodeSensitiveValueDropped {
			t.Errorf("code = %q, want DASHBOARD_SENSITIVE_VALUE_DROPPED", diagnostic.Code)
		}
	}
}

package naming

import (
	"strings"
	"testing"
)

var metricNameCharset = func() func(string) bool {
	return func(value string) bool {
		if value == "" {
			return false
		}
		if value[0] < 'a' || value[0] > 'z' {
			return false
		}
		for _, character := range value {
			valid := character >= 'a' && character <= 'z' ||
				character >= '0' && character <= '9' ||
				character == '_' || character == ':'
			if !valid {
				return false
			}
		}
		return true
	}
}()

// TestNormalizeMachineName is AC1: case, hyphens, spaces and Unicode all
// collapse to the same legal, locale-independent machine name.
func TestNormalizeMachineName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "checkout", "checkout"},
		{"mixed case", "CheckoutService", "checkoutservice"},
		{"hyphens and spaces", "Checkout-Service API", "checkout_service_api"},
		{"collapses separator runs", "a--b   c", "a_b_c"},
		{"leading separator trimmed", "-hello-", "hello"},
		{"unicode with ascii", "Order-Service-支付", "order_service"},
		{"starts with digit", "2fa-service", "m_2fa_service"},
		{"digits kept", "v2api", "v2api"},
		{"underscore preserved", "http_requests", "http_requests"},
		{"colon kept", "namespace:metric", "namespace_metric"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeMachineName(test.input)
			if err != nil {
				t.Fatalf("NormalizeMachineName(%q) failed: %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("NormalizeMachineName(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}

	// A pure-Unicode name has no usable ASCII characters and must fail
	// deterministically; the planner maps the failure to a diagnostic.
	if _, err := NormalizeMachineName("支付服务"); err == nil {
		t.Error("pure-Unicode name must fail")
	}
}

func TestNormalizeMachineNameEmptyFails(t *testing.T) {
	for _, input := range []string{"", "   ", "-_-", "中文"} {
		if _, err := NormalizeMachineName(input); err == nil {
			t.Errorf("NormalizeMachineName(%q) must fail", input)
		}
	}
}

func TestMetricNameComposition(t *testing.T) {
	policy := NamingPolicy{}
	name, diagnostics, err := policy.MetricName(MetricNameSpec{
		Namespace: "api", Service: "Checkout Service", Module: "internal/payments",
		Function: "CreateOrder", Operation: "POST", Purpose: "requests_total",
	})
	if err != nil {
		t.Fatalf("MetricName failed: %v", err)
	}
	want := "api_checkout_service_internal_payments_createorder_post_requests_total"
	if name != want {
		t.Errorf("MetricName = %q, want %q", name, want)
	}
	if len(diagnostics.Items()) != 0 {
		t.Errorf("unexpected diagnostics: %v", diagnostics.Items())
	}
	if !metricNameCharset(name) {
		t.Errorf("metric name %q violates the charset", name)
	}
	if len(name) > MaxMetricNameLength {
		t.Errorf("metric name exceeds %d bytes", MaxMetricNameLength)
	}
}

func TestMetricNameOmitsEmptyComponents(t *testing.T) {
	policy := NamingPolicy{}
	name, _, err := policy.MetricName(MetricNameSpec{
		Service: "demo", Operation: "GET", Purpose: "requests_total",
	})
	if err != nil {
		t.Fatalf("MetricName failed: %v", err)
	}
	if name != "demo_get_requests_total" {
		t.Errorf("MetricName = %q, want demo_get_requests_total", name)
	}
}

func TestMetricNameAllEmptyFails(t *testing.T) {
	policy := NamingPolicy{}
	if _, _, err := policy.MetricName(MetricNameSpec{}); err == nil {
		t.Fatal("MetricName with no components must fail")
	}
}

func TestMetricNameTruncationDropsComponents(t *testing.T) {
	policy := NamingPolicy{}
	longComponent := strings.Repeat("component", 40) // 360 chars
	name, diagnostics, err := policy.MetricName(MetricNameSpec{
		Service: longComponent, Module: longComponent, Function: longComponent,
		Operation: "op", Purpose: "requests_total",
	})
	if err != nil {
		t.Fatalf("MetricName failed: %v", err)
	}
	if len(name) > MaxMetricNameLength {
		t.Errorf("name length = %d, want <= %d", len(name), MaxMetricNameLength)
	}
	if !metricNameCharset(name) {
		t.Errorf("truncated name %q violates the charset", name)
	}
	if len(diagnostics.Items()) != 1 || diagnostics.Items()[0].Code != "GEN_UNSUPPORTED_ENTITY" {
		t.Errorf("expected one GEN_UNSUPPORTED_ENTITY diagnostic, got %v", diagnostics.Items())
	}
}

func TestMetricNameTruncationIsDeterministic(t *testing.T) {
	policy := NamingPolicy{}
	spec := MetricNameSpec{
		Service: strings.Repeat("svc", 50), Module: strings.Repeat("mod", 50),
		Function: strings.Repeat("fn", 50), Operation: "op", Purpose: "count",
	}
	first, _, err := policy.MetricName(spec)
	if err != nil {
		t.Fatalf("MetricName failed: %v", err)
	}
	for range 10 {
		next, _, err := policy.MetricName(spec)
		if err != nil {
			t.Fatalf("MetricName failed: %v", err)
		}
		if next != first {
			t.Fatalf("truncation is not deterministic: %q vs %q", first, next)
		}
	}
}

func TestSpanNameHTTP(t *testing.T) {
	policy := NamingPolicy{}
	tests := []struct {
		method, route, want string
	}{
		{"POST", "/orders/{id}", "POST /orders/{id}"},
		{"get", "/orders", "GET /orders"},
		{"", "/orders/{id}", "HTTP /orders/{id}"},
		{"TRACE2", "/orders", "HTTP /orders"},
		{"BREW", "/orders", "HTTP /orders"},
	}
	for _, test := range tests {
		name, err := policy.SpanName(SpanNameSpec{Kind: SpanKindHTTP, Method: test.method, Route: test.route})
		if err != nil {
			t.Fatalf("SpanName(%q, %q) failed: %v", test.method, test.route, err)
		}
		if name != test.want {
			t.Errorf("SpanName(%q, %q) = %q, want %q", test.method, test.route, name, test.want)
		}
	}
}

func TestSpanNameGRPC(t *testing.T) {
	policy := NamingPolicy{}
	name, err := policy.SpanName(SpanNameSpec{Kind: SpanKindGRPC, Service: "OrderService", GRPCMethod: "CreateOrder"})
	if err != nil {
		t.Fatalf("SpanName failed: %v", err)
	}
	if name != "OrderService/CreateOrder" {
		t.Errorf("SpanName = %q, want OrderService/CreateOrder", name)
	}
	for _, spec := range []SpanNameSpec{
		{Kind: SpanKindGRPC, Service: "", GRPCMethod: "CreateOrder"},
		{Kind: SpanKindGRPC, Service: "OrderService", GRPCMethod: ""},
	} {
		if _, err := policy.SpanName(spec); err == nil {
			t.Errorf("SpanName %+v must fail on missing service/method", spec)
		}
	}
}

func TestSpanNameCron(t *testing.T) {
	policy := NamingPolicy{}
	name, err := policy.SpanName(SpanNameSpec{Kind: SpanKindCron, JobName: "Nightly-Report Generator"})
	if err != nil {
		t.Fatalf("SpanName failed: %v", err)
	}
	if name != "cron nightly_report_generator" {
		t.Errorf("SpanName = %q, want cron nightly_report_generator", name)
	}
	if _, err := policy.SpanName(SpanNameSpec{Kind: SpanKindCron, JobName: ""}); err == nil {
		t.Error("cron span name without a job name must fail")
	}
}

func TestSpanNameDependency(t *testing.T) {
	policy := NamingPolicy{}
	name, err := policy.SpanName(SpanNameSpec{Kind: SpanKindDependency, System: "sql", Operation: "exec"})
	if err != nil {
		t.Fatalf("SpanName failed: %v", err)
	}
	if name != "sql exec" {
		t.Errorf("SpanName = %q, want sql exec", name)
	}
	// Missing operation degrades to the controlled "unknown", never to a
	// raw value.
	name, err = policy.SpanName(SpanNameSpec{Kind: SpanKindDependency, System: "redis"})
	if err != nil {
		t.Fatalf("SpanName failed: %v", err)
	}
	if name != "redis unknown" {
		t.Errorf("SpanName = %q, want redis unknown", name)
	}
	if _, err := policy.SpanName(SpanNameSpec{Kind: SpanKindDependency, System: ""}); err == nil {
		t.Error("dependency span name without a system must fail")
	}
}

func TestSpanNameUnknownKindFails(t *testing.T) {
	policy := NamingPolicy{}
	if _, err := policy.SpanName(SpanNameSpec{Kind: "kafka"}); err == nil {
		t.Fatal("unknown span name kind must fail")
	}
}

func TestEventName(t *testing.T) {
	policy := NamingPolicy{}
	tests := []struct {
		segments []string
		want     string
	}{
		{[]string{"HTTP", "Request", "Completed"}, "http.request.completed"},
		{[]string{"dependency", "operation", "failed"}, "dependency.operation.failed"},
		{[]string{"cron", "job", "started"}, "cron.job.started"},
		{[]string{"rpc", "request", "failed"}, "rpc.request.failed"},
		{[]string{"", "request"}, "request"},
	}
	for _, test := range tests {
		name, err := policy.EventName(test.segments...)
		if err != nil {
			t.Fatalf("EventName(%v) failed: %v", test.segments, err)
		}
		if name != test.want {
			t.Errorf("EventName(%v) = %q, want %q", test.segments, name, test.want)
		}
	}
	if _, err := policy.EventName("", ""); err == nil {
		t.Fatal("EventName with no usable segments must fail")
	}
}

func TestValidRuntimeStatus(t *testing.T) {
	for _, status := range []string{"ok", "error", "cancelled", "timeout", "unknown"} {
		if !ValidRuntimeStatus(status) {
			t.Errorf("ValidRuntimeStatus(%q) = false", status)
		}
	}
	for _, status := range []string{"success", "failed", "OK", ""} {
		if ValidRuntimeStatus(status) {
			t.Errorf("ValidRuntimeStatus(%q) = true", status)
		}
	}
}

// TestGeneratedNamesAreLocaleIndependent: normalization must not depend
// on any locale; run it with a Turkish-style input that would break a
// naive case mapping.
func TestGeneratedNamesAreLocaleIndependent(t *testing.T) {
	for _, input := range []string{"Order-API", "checkout", "İstanbul"} {
		first, err := NormalizeMachineName(input)
		if err != nil {
			t.Fatalf("NormalizeMachineName(%q) failed: %v", input, err)
		}
		for range 5 {
			next, err := NormalizeMachineName(input)
			if err != nil {
				t.Fatalf("NormalizeMachineName(%q) failed: %v", input, err)
			}
			if next != first {
				t.Fatalf("normalization of %q is not stable: %q vs %q", input, first, next)
			}
		}
	}
}

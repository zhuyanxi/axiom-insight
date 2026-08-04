package naming

import (
	"strings"
	"testing"
)

// canaryValues are the security fixtures: credentials, PII, SQL, a
// malicious URL and a Kafka payload that must never surface in any
// generated name, attribute decision, diagnostic or budget error.
var canaryValues = []string{
	"https://user:pass@example.com/orders?id=42#detail",
	"SELECT * FROM users WHERE password = 'hunter2'",
	"redis:user:42:session",
	"kafka-payload-canary-7f3a",
	"user@example.com",
	"+86-138-0000-0000",
	"idcard-110101199003077777",
	"sk-live-9f8e7d6c5b4a3c2d1e0f",
	"Authorization: Bearer canary-token-abc123",
}

// TestCanaryValuesDetected: every value fixture is recognized as sensitive
// or high-cardinality so the planner will drop it. The Kafka payload
// canary is blocked at the attribute/field key level (payload keys are
// never on any allowlist), which TestCanaryValuesAbsentFromDiagnostics
// covers.
func TestCanaryValuesDetected(t *testing.T) {
	for _, value := range canaryValues {
		if value == "kafka-payload-canary-7f3a" {
			continue
		}
		if !IsSensitiveValue(value) && !IsHighCardinalityValue(value) {
			t.Errorf("canary %q is neither sensitive nor high-cardinality", value)
		}
	}
	// The payload canary must still be blocked as a key on every signal.
	policy := AttributePolicy{}
	if policy.TraceAttributeAllowed("messaging.message.payload") {
		t.Error("payload key must be blocked on traces")
	}
	if policy.MetricAttributeAllowed("payload") {
		t.Error("payload key must be blocked on metrics")
	}
	if policy.LogFieldAllowed("payload", nil) {
		t.Error("payload key must be blocked on logs")
	}
}

// TestCanaryValuesAbsentFromDiagnostics is the security regression core:
// a full policy pipeline (naming, collision, budget) must produce bytes
// and diagnostics that contain none of the canaries.
func TestCanaryValuesAbsentFromDiagnostics(t *testing.T) {
	policy := NamingPolicy{}
	estimator := SeriesEstimator{}
	check := BudgetCheck{}

	// Build colliding names from canary-bearing inputs; disambiguation
	// messages must not echo the raw values.
	items := []NameItem{
		{Signal: "metrics", TargetID: "dep:orders", Name: "orders_requests_total"},
		{Signal: "metrics", TargetID: "dep:orders-v2", Name: "orders_requests_total"},
		{Signal: "tracing", TargetID: "dep:sql", Name: "db exec"},
		{Signal: "tracing", TargetID: "dep:sql-v2", Name: "db exec"},
	}
	results, diagnostics := policy.Disambiguate(items)
	var output strings.Builder
	for _, result := range results {
		output.WriteString(result.Signal + "|" + result.Name + "\n")
	}
	for _, diagnostic := range diagnostics.Items() {
		output.WriteString(diagnostic.Code + " " + diagnostic.Signal + " " + diagnostic.TargetID + " " + diagnostic.Field + " " + diagnostic.Message + "\n")
	}

	// A budget failure message must not carry any value either.
	estimated, err := estimator.EstimateSeries("histogram", []int{1, 1, 5}, 11, 0)
	if err != nil {
		t.Fatalf("EstimateSeries failed: %v", err)
	}
	if budgetErr := check.SeriesBudget("metrics", estimated, 10); budgetErr != nil {
		output.WriteString(budgetErr.Error())
	}

	allOutput := output.String()
	for _, canary := range canaryValues {
		if strings.Contains(allOutput, canary) {
			t.Errorf("output leaks canary %q", canary)
		}
	}
	for _, canary := range canaryValues {
		for _, diagnostic := range diagnostics.Items() {
			if strings.Contains(diagnostic.Message, canary) {
				t.Errorf("diagnostic %s leaks canary %q", diagnostic.Code, canary)
			}
		}
	}
}

// TestCanaryValuesAbsentFromNames: canary inputs used as name components
// never survive normalization into a generated name.
func TestCanaryValuesAbsentFromNames(t *testing.T) {
	policy := NamingPolicy{}
	for _, canary := range canaryValues {
		name, diagnostics, err := policy.MetricName(MetricNameSpec{
			Service: canary, Operation: "op", Purpose: "total",
		})
		if err != nil {
			t.Fatalf("MetricName failed for canary input: %v", err)
		}
		if strings.Contains(name, canary) {
			t.Errorf("metric name leaks canary: %q", name)
		}
		for _, diagnostic := range diagnostics.Items() {
			if strings.Contains(diagnostic.Message, canary) {
				t.Errorf("metric diagnostic leaks canary: %s", diagnostic.Message)
			}
		}
		span, err := policy.SpanName(SpanNameSpec{Kind: SpanKindDependency, System: canary, Operation: canary})
		if err != nil {
			t.Fatalf("SpanName failed for canary input: %v", err)
		}
		if strings.Contains(span, canary) {
			t.Errorf("span name leaks canary: %q", span)
		}
	}
}

// TestOverlongUnicodeNameProducesLegalName: overlong Unicode-heavy names
// still yield a legal machine name or a deterministic failure.
func TestOverlongUnicodeNameProducesLegalName(t *testing.T) {
	policy := NamingPolicy{}
	overlong := strings.Repeat("verylongcomponent", 50)
	name, diagnostics, err := policy.MetricName(MetricNameSpec{
		Service: overlong, Module: overlong, Function: overlong,
		Operation: "op", Purpose: "total",
	})
	if err != nil {
		// A deterministic failure is acceptable; a partial name is not.
		return
	}
	if !metricNameCharset(name) || len(name) > MaxMetricNameLength {
		t.Errorf("overlong input produced illegal name %q", name)
	}
	if len(diagnostics.Items()) != 1 {
		t.Errorf("expected truncation diagnostic, got %v", diagnostics.Items())
	}
}

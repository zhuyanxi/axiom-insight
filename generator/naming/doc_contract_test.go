package naming

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDocumentationMentionsEveryContractCode: the policy documentation
// must cover the naming rules, both allowlists, the denylist, the
// collision policy and the series budget, so the contract cannot drift
// from the code silently.
func TestDocumentationMentionsEveryContractCode(t *testing.T) {
	content := readNamingDocument(t)
	required := []string{
		"NormalizeMachineName",
		"Metric 名称",
		"Span 名称",
		"Log Event 名称",
		"属性 allowlist",
		"credential denylist",
		"GEN_NAME_COLLISION",
		"GEN_CARDINALITY_BLOCKED",
		"GEN_SENSITIVE_VALUE_DROPPED",
		"GEN_CARDINALITY_LIMIT_EXCEEDED",
		"GEN_UNSUPPORTED_ENTITY",
		"GEN_INCOMPLETE_TARGET",
		"sha256",
		"strict",
		"classic-exposition",
	}
	for _, fragment := range required {
		if !containsFragment(content, fragment) {
			t.Errorf("documentation lacks required fragment %q", fragment)
		}
	}
}

// TestDocumentationExamplesMatchImplementation: the documented name
// examples must match the actual policy output.
func TestDocumentationExamplesMatchImplementation(t *testing.T) {
	tests := []struct {
		docExample string
		got        string
	}{
		{"checkout_service_api", mustNormalize(t, "Checkout-Service API")},
		{"m_2fa_service", mustNormalize(t, "2fa-service")},
		{"order_service", mustNormalize(t, "Order-Service-支付")},
		{"cron nightly_report_generator", mustSpan(t, SpanNameSpec{Kind: SpanKindCron, JobName: "Nightly-Report Generator"})},
		{"sql exec", mustSpan(t, SpanNameSpec{Kind: SpanKindDependency, System: "sql", Operation: "exec"})},
		{"POST /orders/{id}", mustSpan(t, SpanNameSpec{Kind: SpanKindHTTP, Method: "POST", Route: "/orders/{id}"})},
		{"OrderService/CreateOrder", mustSpan(t, SpanNameSpec{Kind: SpanKindGRPC, Service: "OrderService", GRPCMethod: "CreateOrder"})},
		{"http.request.completed", mustEvent(t, "HTTP", "Request", "Completed")},
		{"dependency.operation.failed", mustEvent(t, "dependency", "operation", "failed")},
		{"cron.job.started", mustEvent(t, "cron", "job", "started")},
	}
	for _, test := range tests {
		if test.got != test.docExample {
			t.Errorf("documentation example %q does not match implementation %q", test.docExample, test.got)
		}
	}
}

func readNamingDocument(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "06-naming-policy.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read naming documentation: %v", err)
	}
	return string(data)
}

func containsFragment(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}

func mustNormalize(t *testing.T, value string) string {
	t.Helper()
	normalized, err := NormalizeMachineName(value)
	if err != nil {
		t.Fatalf("NormalizeMachineName(%q) failed: %v", value, err)
	}
	return normalized
}

func mustSpan(t *testing.T, spec SpanNameSpec) string {
	t.Helper()
	name, err := NamingPolicy{}.SpanName(spec)
	if err != nil {
		t.Fatalf("SpanName(%+v) failed: %v", spec, err)
	}
	return name
}

func mustEvent(t *testing.T, segments ...string) string {
	t.Helper()
	name, err := NamingPolicy{}.EventName(segments...)
	if err != nil {
		t.Fatalf("EventName(%v) failed: %v", segments, err)
	}
	return name
}

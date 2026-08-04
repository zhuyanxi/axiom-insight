package naming

import (
	"strings"
	"testing"
)

func TestMetricAttributeAllowlist(t *testing.T) {
	policy := AttributePolicy{}
	for _, key := range []string{"service", "operation", "status"} {
		if !policy.MetricAttributeAllowed(key) {
			t.Errorf("MetricAttributeAllowed(%q) = false", key)
		}
	}
	for _, key := range []string{"module", "function", "version", "url", "route", "path", "sql", "query", "key", "payload", "error"} {
		if policy.MetricAttributeAllowed(key) {
			t.Errorf("MetricAttributeAllowed(%q) = true, want false", key)
		}
	}
}

func TestGaugeAttributeAllowlist(t *testing.T) {
	policy := AttributePolicy{}
	if !policy.GaugeAttributeAllowed("service") || !policy.GaugeAttributeAllowed("operation") {
		t.Error("gauge must allow service and operation")
	}
	if policy.GaugeAttributeAllowed("status") {
		t.Error("gauge must never carry status")
	}
}

func TestTraceAttributeAllowlist(t *testing.T) {
	policy := AttributePolicy{}
	allowed := []string{
		"service.name", "service.version", "operation",
		"code.namespace", "code.function",
		"http.request.method", "http.route",
		"rpc.system", "rpc.service", "rpc.method",
		"db.system", "db.operation",
		"messaging.system", "messaging.operation",
		"cron.job.name", "cron.job.schedule",
	}
	for _, key := range allowed {
		if !policy.TraceAttributeAllowed(key) {
			t.Errorf("TraceAttributeAllowed(%q) = false, want true", key)
		}
	}
}

// TestTraceAttributeAC4: every component of the canary URL
// https://user:pass@example.com/orders?id=42#detail must be rejected as
// an attribute key.
func TestTraceAttributeAC4(t *testing.T) {
	policy := AttributePolicy{}
	for _, key := range []string{
		"url.full", "url.scheme", "url.host", "url.path", "url.query",
		"url.fragment", "url.userinfo",
		"http.url", "http.target", "http.query_string",
		"http.request.header.authorization",
		"http.request.cookie",
		"db.statement", "db.redis.key",
		"messaging.destination", "messaging.destination.topic",
		"messaging.message.body", "messaging.message.payload",
		"server.address", "server.port",
		"net.peer.name", "net.peer.host",
		"enduser.id", "enduser.email",
	} {
		if policy.TraceAttributeAllowed(key) {
			t.Errorf("TraceAttributeAllowed(%q) = true, want false", key)
		}
		if blocked, _ := policy.BlockedTraceAttribute(key); !blocked {
			t.Errorf("BlockedTraceAttribute(%q) reported not blocked", key)
		}
	}
}

// TestBlockedTraceAttributePrefixes: header/cookie/payload families are
// blocked by prefix, including keys the allowlist does not name.
func TestBlockedTraceAttributePrefixes(t *testing.T) {
	policy := AttributePolicy{}
	for _, key := range []string{
		"http.request.header.x-api-key",
		"http.request.header.cookie",
		"http.response.header.set-cookie",
		"messaging.message.body.utf8",
		"db.connection_string.foo",
	} {
		if policy.TraceAttributeAllowed(key) {
			t.Errorf("TraceAttributeAllowed(%q) = true, want false", key)
		}
	}
}

func TestNormalizeFieldKey(t *testing.T) {
	tests := []struct{ input, want string }{
		{"Authorization", "authorization"},
		{"AUTH-TOKEN", "auth_token"},
		{"auth_token", "auth_token"},
		{"  User-ID  ", "user_id"},
	}
	for _, test := range tests {
		if got := NormalizeFieldKey(test.input); got != test.want {
			t.Errorf("NormalizeFieldKey(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestLogFieldAllowlist(t *testing.T) {
	policy := AttributePolicy{}
	for _, key := range []string{
		"timestamp", "event.name", "service", "module", "function",
		"operation", "status", "version", "duration_seconds",
		"error.type", "request_id", "trace_id", "span_id",
		"method", "route", "rpc.service", "cron.job.name", "system",
	} {
		if !policy.LogFieldAllowed(key, nil) {
			t.Errorf("LogFieldAllowed(%q) = false, want true", key)
		}
	}
	for _, key := range []string{
		"body", "payload", "sql", "query", "url", "path", "headers",
		"raw", "message",
	} {
		if policy.LogFieldAllowed(key, nil) {
			t.Errorf("LogFieldAllowed(%q) = true, want false (not on allowlist)", key)
		}
	}
}

// TestLogFieldCredentialDenylist is AC5: the built-in denylist cannot be
// bypassed by case, hyphen/underscore variation or any redaction
// configuration.
func TestLogFieldCredentialDenylist(t *testing.T) {
	policy := AttributePolicy{}
	denied := []string{
		"authorization", "Authorization", "AUTH-TOKEN", "auth_token",
		"cookie", "COOKIE",
		"password", "PASSWORD", "user_password", "passwd",
		"secret", "client_secret",
		"token", "access_token", "id_token",
		"api_key", "API-Key", "user_email", "email_address",
		"phone_number", "mobile", "id_card", "identity_no",
		"credit_card_number", "session_id", "private_key",
	}
	for _, key := range denied {
		if policy.LogFieldAllowed(key, nil) {
			t.Errorf("LogFieldAllowed(%q) = true, want denylist rejection", key)
		}
		if !IsSensitiveFieldKey(key) {
			t.Errorf("IsSensitiveFieldKey(%q) = false", key)
		}
	}
}

// TestLogFieldUserRedactFields: configured redaction names add to the
// denylist but cannot remove it.
func TestLogFieldUserRedactFields(t *testing.T) {
	policy := AttributePolicy{}
	redact := []string{"request_id", "DURATION-SECONDS"}
	if policy.LogFieldAllowed("request_id", redact) {
		t.Error("configured redact field request_id must be denied")
	}
	if policy.LogFieldAllowed("duration_seconds", redact) {
		t.Error("configured redact field duration_seconds (dash variant) must be denied")
	}
	if !policy.LogFieldAllowed("trace_id", redact) {
		t.Error("unconfigured field trace_id must stay allowed")
	}
	if policy.LogFieldAllowed("password", redact) {
		t.Error("built-in denylist entry must stay denied even when configured")
	}
}

func TestIsSensitiveValue(t *testing.T) {
	sensitive := []string{
		"password=hunter2",
		"Bearer sk-secret123",
		"api_key=abcdef",
		"Authorization: Basic dXNlcjpwYXNz",
		"user@example.com",
		"-----BEGIN PRIVATE KEY-----",
		"client_secret=xyz",
	}
	for _, value := range sensitive {
		if !IsSensitiveValue(value) {
			t.Errorf("IsSensitiveValue(%q) = false, want true", value)
		}
	}
	benign := []string{"GET /orders", "redis get", "ok", "1", ""}
	for _, value := range benign {
		if IsSensitiveValue(value) {
			t.Errorf("IsSensitiveValue(%q) = true, want false", value)
		}
	}
}

func TestIsHighCardinalityValue(t *testing.T) {
	high := []string{
		"https://example.com/orders?id=42",
		"SELECT * FROM users WHERE id = ?",
		"/orders/42",
		"redis:user:42:session",
	}
	for _, value := range high {
		if !IsHighCardinalityValue(value) {
			t.Errorf("IsHighCardinalityValue(%q) = false, want true", value)
		}
	}
	low := []string{"POST", "exec", "orders", "kafka"}
	for _, value := range low {
		if IsHighCardinalityValue(value) {
			t.Errorf("IsHighCardinalityValue(%q) = true, want false", value)
		}
	}
}

// TestDiagnosticsNeverContainValues: blocked attributes and collisions
// produce messages that cannot echo the rejected value.
func TestDiagnosticsNeverContainValues(t *testing.T) {
	canary := "canary-9f8e7d6c-secret-value"
	policy := NamingPolicy{}
	items := []NameItem{
		{Signal: "metrics", TargetID: "dep:a", Name: canary},
		{Signal: "metrics", TargetID: "dep:b", Name: canary},
	}
	results, diagnostics := policy.Disambiguate(items)
	if !ContainsName(results, canary) {
		t.Fatal("base name must survive disambiguation")
	}
	for _, diagnostic := range diagnostics.Items() {
		if strings.Contains(diagnostic.Message, canary) {
			t.Errorf("diagnostic leaks the value: %s", diagnostic.Message)
		}
	}
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
}

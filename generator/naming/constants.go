package naming

// Name length limits. Names are truncated deterministically beyond these
// bounds; limits are part of the published contract.
const (
	// MaxMetricNameLength matches the OpenMetrics limit of 200 bytes.
	MaxMetricNameLength = 200
	// MaxSpanNameLength bounds span names (free-form but bounded).
	MaxSpanNameLength = 256
	// MaxEventNameLength bounds structured log event names.
	MaxEventNameLength = 200
	// MaxComponentLength bounds a single normalized name component; the
	// whole-name limit is enforced after component joining.
	MaxComponentLength = 128
	// CollisionSuffixLength is the hex length of the disambiguation
	// suffix: SHA-256(target ID) truncated to this many hex digits.
	CollisionSuffixLength = 8
)

// Runtime status vocabulary (finite, exactly these five). Mirrors the
// generator document contract; kept local so this package stays
// independent of the generated-document models.
const (
	RuntimeStatusOK        = "ok"
	RuntimeStatusError     = "error"
	RuntimeStatusCancelled = "cancelled"
	RuntimeStatusTimeout   = "timeout"
	RuntimeStatusUnknown   = "unknown"
)

var runtimeStatuses = map[string]bool{
	RuntimeStatusOK:        true,
	RuntimeStatusError:     true,
	RuntimeStatusCancelled: true,
	RuntimeStatusTimeout:   true,
	RuntimeStatusUnknown:   true,
}

// Metric type names for series estimation. Mirrors the generator document
// contract vocabulary.
const (
	MetricTypeCounter   = "counter"
	MetricTypeHistogram = "histogram"
	MetricTypeGauge     = "gauge"
	MetricTypeSummary   = "summary"
)

// Default metric attribute keys: exactly service, operation, status.
// Gauges never carry status. Module and function enter stable names and
// target metadata instead of attributes; version stays out of metrics.
const (
	AttributeService   = "service"
	AttributeOperation = "operation"
	AttributeStatus    = "status"
)

var metricAttributeKeys = map[string]bool{
	AttributeService:   true,
	AttributeOperation: true,
	AttributeStatus:    true,
}

// Trace attribute allowlist (fixed vocabulary, pinned semantic convention
// naming where applicable). Dependency URLs, hosts, targets, resources
// and values are never allowed; only low-risk identity keys are.
var traceAttributeKeys = map[string]bool{
	"service.name":          true,
	"service.version":       true,
	"operation":             true,
	"code.namespace":        true,
	"code.function":         true,
	"http.request.method":   true,
	"http.route":            true,
	"rpc.system":            true,
	"rpc.service":           true,
	"rpc.method":            true,
	"db.system":             true,
	"db.operation":          true,
	"messaging.system":      true,
	"messaging.operation":   true,
	"cron.job.name":         true,
	"cron.job.schedule":     true,
	"server.system":         true,
	"span.scope":            true,
	"exception.type":        true,
}

// Blocked trace attribute key prefixes and exact names. Kept explicit so
// security tests can assert that URL components, SQL, keys, payloads,
// addresses and PII can never be introduced by any attribute key.
var blockedTraceAttributeNames = map[string]bool{
	"url.full":          true,
	"url.scheme":        true,
	"url.host":          true,
	"url.path":          true,
	"url.query":         true,
	"url.fragment":      true,
	"url.userinfo":      true,
	"http.url":          true,
	"http.target":       true,
	"http.query_string": true,
	"db.statement":      true,
	"db.redis.key":      true,
	"messaging.destination":            true,
	"messaging.destination.topic":      true,
	"messaging.consumer.group":         true,
	"messaging.message.payload":        true,
	"server.address":   true,
	"server.port":      true,
	"net.peer.name":    true,
	"net.peer.host":    true,
	"net.peer.address": true,
	"enduser.id":          true,
	"enduser.email":       true,
	"enduser.phone":       true,
	"exception.message":   true,
	"exception.stacktrace": true,
}

var blockedTraceAttributePrefixes = []string{
	"http.request.header.",
	"http.response.header.",
	"http.request.cookie",
	"db.connection_string",
	"messaging.message.body",
}

// Log field allowlist: controlled keys only. Anything else (including any
// raw payload, URL, SQL or key field) is rejected by the allowlist, and
// anything on the credential denylist is rejected no matter what.
var logFieldKeys = map[string]bool{
	"timestamp":         true,
	"event.name":        true,
	"service":           true,
	"module":            true,
	"function":          true,
	"operation":         true,
	"status":            true,
	"version":           true,
	"duration_seconds":  true,
	"error.type":        true,
	"error.category":    true,
	"request_id":        true,
	"trace_id":          true,
	"span_id":           true,
	"method":            true,
	"route":             true,
	"rpc.service":       true,
	"rpc.method":        true,
	"cron.job.name":     true,
	"system":            true,
	"db.system":         true,
}

// builtinDenylistKeys are the normalized credential field names that can
// never be logged, matching the unclosable denylist of the generation
// Policy (P1-03). Field-key matching is case-insensitive and treats '-'
// and '_' as equal, so "Authorization", "AUTH-TOKEN" and "auth_token" all
// match.
var builtinDenylistKeys = []string{
	"authorization",
	"cookie",
	"password",
	"secret",
	"token",
}

// sensitiveKeyPatterns extend the denylist with PII and credential field
// names. Matching is a normalized substring test, so "user_email" is
// caught by "email" and "api_key" by "api_key".
var sensitiveKeyPatterns = []string{
	"email",
	"mail",
	"phone",
	"mobile",
	"tel",
	"id_card",
	"identity",
	"passport",
	"ssn",
	"social_security",
	"credit_card",
	"card_number",
	"cvv",
	"pin",
	"api_key",
	"apikey",
	"session",
	"auth",
	"credential",
	"private_key",
	"password",
	"passwd",
	"secret",
	"token",
	"cookie",
}

// sensitiveValuePatterns detect values that must never leave the plan.
// Matching is a lowercase substring test; the matcher is conservative
// (false positives are safer than leaks because the value is dropped).
var sensitiveValuePatterns = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"apikey",
	"api_key",
	"authorization",
	"bearer",
	"private key",
	"-----begin",
	"sk-",
}

// spanKind names recognized by SpanName construction.
const (
	SpanKindHTTP     = "http"
	SpanKindGRPC     = "grpc"
	SpanKindCron     = "cron"
	SpanKindDependency = "dependency"
)

// HTTP method vocabulary used by HTTP span names. Any other method token
// falls back to the controlled "HTTP" prefix.
var httpMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true,
	"DELETE": true, "CONNECT": true, "OPTIONS": true, "TRACE": true,
	"PATCH": true,
}

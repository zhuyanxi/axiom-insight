package naming

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// NamingPolicy builds deterministic machine names from IR stable fields.
// It is stateless: every method is a pure function of its inputs.
type NamingPolicy struct{}

// MetricNameSpec carries the stable components of one instrument name.
// Empty components are omitted; components are normalized individually.
type MetricNameSpec struct {
	// Namespace is the optional metrics namespace prefix.
	Namespace string
	// Service is service.name.
	Service string
	// Module is the normalized function package path.
	Module string
	// Function is the qualified function name.
	Function string
	// Operation is the endpoint or dependency operation.
	Operation string
	// Purpose is the purpose suffix, e.g. "requests_total" or
	// "request_duration_seconds".
	Purpose string
}

// MetricName composes and normalizes a metric name:
// <namespace>_<service>_<module>_<function>_<operation>_<purpose>, all in
// ASCII snake_case. The result matches ^[a-z][a-z0-9_:]*$ and is at most
// MaxMetricNameLength bytes. When the name exceeds the limit the least
// specific components (function, then module) are dropped first; a final
// deterministic SHA-256 suffix keeps the truncated name stable. Any such
// degradation is reported as a warning diagnostic.
func (NamingPolicy) MetricName(spec MetricNameSpec) (string, *DiagnosticList, error) {
	components := make([]string, 0, 6)
	for _, raw := range []string{spec.Namespace, spec.Service, spec.Module, spec.Function, spec.Operation, spec.Purpose} {
		component, err := NormalizeMachineName(raw)
		if err != nil {
			continue
		}
		components = append(components, component)
	}
	if len(components) == 0 {
		return "", nil, fmt.Errorf("metric name has no non-empty components")
	}

	diagnostics := new(DiagnosticList)
	joined := strings.Join(components, "_")
	if len(joined) > MaxMetricNameLength {
		// Drop the least specific components deterministically, then
		// truncate with a stable hash suffix.
		for len(components) > 2 && len(joined) > MaxMetricNameLength {
			components = components[2:]
			joined = strings.Join(components, "_")
		}
		if len(joined) > MaxMetricNameLength {
			suffix := shortHash(joined)
			keep := max(MaxMetricNameLength-len(suffix)-1, 1)
			joined = joined[:keep] + "_" + suffix
		}
		diagnostics.Add(policy.CodeUnsupportedEntity, "metrics", spec.Service,
			"name", fmt.Sprintf("metric name exceeds %d bytes and was deterministically truncated", MaxMetricNameLength))
	}
	return joined, diagnostics, nil
}

// SpanNameSpec carries the components for one span name.
type SpanNameSpec struct {
	// Kind selects the naming rule: http, grpc, cron or dependency.
	Kind string
	// Method is the HTTP method (uppercase).
	Method string
	// Route is the HTTP route template, e.g. "/orders/{id}".
	Route string
	// Service is the gRPC service name.
	Service string
	// GRPCMethod is the gRPC method name.
	GRPCMethod string
	// JobName is the stable cron job name.
	JobName string
	// System is the dependency system, e.g. "sql" or "redis".
	System string
	// Operation is the dependency operation.
	Operation string
}

// SpanName builds a span name per the contract rules:
//
//	http:       <METHOD> <route-template>, or "HTTP <route-template>"
//	            when the method is unknown or empty
//	grpc:       <service>/<method>
//	cron:       "cron <stable-job-name>"
//	dependency: <system> <operation>
//
// Raw target values (URLs, hosts, keys) never appear in the result.
func (NamingPolicy) SpanName(spec SpanNameSpec) (string, error) {
	switch spec.Kind {
	case SpanKindHTTP:
		method := strings.ToUpper(strings.TrimSpace(spec.Method))
		if !httpMethods[method] {
			method = "HTTP"
		}
		return bounded(fmt.Sprintf("%s %s", method, strings.TrimSpace(spec.Route)), MaxSpanNameLength)
	case SpanKindGRPC:
		service := strings.TrimSpace(spec.Service)
		method := strings.TrimSpace(spec.GRPCMethod)
		if service == "" || method == "" {
			return "", fmt.Errorf("gRPC span name requires both service and method")
		}
		return bounded(service+"/"+method, MaxSpanNameLength)
	case SpanKindCron:
		job, err := NormalizeMachineName(spec.JobName)
		if err != nil {
			return "", fmt.Errorf("cron span name requires a stable job name")
		}
		return bounded("cron "+job, MaxSpanNameLength)
	case SpanKindDependency:
		system, err := NormalizeMachineName(spec.System)
		if err != nil {
			return "", fmt.Errorf("dependency span name requires a system")
		}
		operation, operationErr := NormalizeMachineName(spec.Operation)
		if operationErr != nil {
			operation = "unknown"
		}
		return bounded(system+" "+operation, MaxSpanNameLength)
	default:
		return "", fmt.Errorf("unsupported span name kind %q", spec.Kind)
	}
}

// EventName builds a structured log event name: lowercase dotted
// segments, e.g. "http.request.completed" or "dependency.operation.failed".
// Empty segments are omitted; segments are normalized to ASCII snake_case.
func (NamingPolicy) EventName(segments ...string) (string, error) {
	normalized := make([]string, 0, len(segments))
	for _, segment := range segments {
		value, err := NormalizeMachineName(segment)
		if err != nil {
			continue
		}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return "", fmt.Errorf("event name has no non-empty segments")
	}
	return bounded(strings.Join(normalized, "."), MaxEventNameLength)
}

// NormalizeMachineName converts an arbitrary IR string into an ASCII
// machine name: lowercase (Unicode-aware, locale-independent), runs of
// non-alphanumeric characters collapsed to a single underscore, prefixed
// with "m_" when it would not start with an ASCII letter, and bounded to
// MaxComponentLength bytes. An empty result is an error.
func NormalizeMachineName(value string) (string, error) {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			builder.WriteRune(character)
		case character >= 'A' && character <= 'Z':
			builder.WriteRune(unicode.ToLower(character))
		case character >= '0' && character <= '9':
			builder.WriteRune(character)
		default:
			// Collapse every run of separators into a single underscore.
			written := builder.String()
			if written == "" || written[len(written)-1] == '_' {
				continue
			}
			builder.WriteByte('_')
		}
	}
	normalized := strings.Trim(builder.String(), "_")
	if normalized == "" {
		return "", fmt.Errorf("name has no usable characters")
	}
	if normalized[0] < 'a' || normalized[0] > 'z' {
		normalized = "m_" + normalized
	}
	if len(normalized) > MaxComponentLength {
		normalized = normalized[:MaxComponentLength]
	}
	return normalized, nil
}

// ValidRuntimeStatus reports whether value is one of the five finite
// runtime statuses.
func ValidRuntimeStatus(value string) bool {
	return runtimeStatuses[value]
}

// bounded enforces a byte limit on a generated name.
func bounded(value string, limit int) (string, error) {
	if value == "" {
		return "", fmt.Errorf("generated name is empty")
	}
	if len(value) <= limit {
		return value, nil
	}
	return value[:limit], nil
}

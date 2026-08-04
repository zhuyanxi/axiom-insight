package naming

import (
	"regexp"
	"slices"
	"strings"
)

// AttributePolicy decides which attribute keys, field names and values
// may enter a plan. It is stateless: every method is a pure function.
type AttributePolicy struct{}

// MetricAttributeAllowed reports whether key may appear on a metric.
// The default vocabulary is exactly service, operation and status; gauges
// never carry status (use GaugeAttributeAllowed).
func (AttributePolicy) MetricAttributeAllowed(key string) bool {
	return metricAttributeKeys[key]
}

// GaugeAttributeAllowed reports whether key may appear on a gauge:
// service and operation only, never status.
func (AttributePolicy) GaugeAttributeAllowed(key string) bool {
	return key == AttributeService || key == AttributeOperation
}

// TraceAttributeAllowed reports whether key may appear on a span
// attribute. The allowlist is positive and closed: anything not listed is
// rejected, so URL components, SQL text, keys, payloads, addresses and
// PII cannot enter a plan by renaming alone.
func (AttributePolicy) TraceAttributeAllowed(key string) bool {
	if traceAttributeKeys[key] {
		return true
	}
	if blockedTraceAttributeNames[key] {
		return false
	}
	for _, prefix := range blockedTraceAttributePrefixes {
		if strings.HasPrefix(key, prefix) {
			return false
		}
	}
	return false
}

// BlockedTraceAttribute reports why a trace attribute key is rejected.
// It exists for security tests and diagnostics; the returned reason never
// contains values.
func (AttributePolicy) BlockedTraceAttribute(key string) (blocked bool, reason string) {
	if blockedTraceAttributeNames[key] {
		return true, "blocked trace attribute name"
	}
	for _, prefix := range blockedTraceAttributePrefixes {
		if strings.HasPrefix(key, prefix) {
			return true, "blocked trace attribute prefix"
		}
	}
	return false, ""
}

// NormalizeFieldKey lowercases a log field key and treats '-' and '_' as
// equal, so "AUTH-TOKEN", "auth_token" and "Authorization" all normalize
// to the same key.
func NormalizeFieldKey(key string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), "-", "_")
}

// LogFieldAllowed reports whether key may be a structured log field: it
// must be on the controlled allowlist, must not match the unclosable
// built-in credential denylist or PII patterns, and must not be listed in
// the configured redaction names (normalized). The user cannot remove
// denylist entries; they can only add further names.
func (AttributePolicy) LogFieldAllowed(key string, redactFields []string) bool {
	if !logFieldKeys[key] {
		return false
	}
	if IsSensitiveFieldKey(key) {
		return false
	}
	normalized := NormalizeFieldKey(key)
	return !slices.ContainsFunc(redactFields, func(redact string) bool {
		return NormalizeFieldKey(redact) == normalized
	})
}

// IsSensitiveFieldKey reports whether a field key matches the unclosable
// credential denylist or a PII pattern, normalized. The built-in denylist
// of the generation Policy (P1-03) is a subset of these names.
func IsSensitiveFieldKey(key string) bool {
	normalized := NormalizeFieldKey(key)
	if slices.Contains(builtinDenylistKeys, normalized) {
		return true
	}
	for _, pattern := range sensitiveKeyPatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

// IsSensitiveValue reports whether a value looks like a credential or PII
// and must be dropped. The matcher is deliberately conservative: values
// are dropped, never rewritten, so a false positive only costs a
// diagnostic while a false negative would leak.
func IsSensitiveValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	for _, pattern := range sensitiveValuePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	if emailPattern.MatchString(lower) {
		return true
	}
	if privateKeyPattern.MatchString(lower) {
		return true
	}
	if phonePattern.MatchString(lower) {
		return true
	}
	if longDigitPattern.MatchString(lower) {
		return true
	}
	return false
}

// IsHighCardinalityValue reports whether a value looks like a raw
// location, payload or resource detail (URL, query, SQL, key, path) that
// must never become a metric attribute or trace attribute value.
func IsHighCardinalityValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(lower, "://") || strings.HasPrefix(lower, "/") {
		return true
	}
	if strings.Contains(lower, "?") && strings.Contains(lower, "=") {
		return true
	}
	if strings.Contains(lower, "select ") || strings.Contains(lower, "insert ") ||
		strings.Contains(lower, "update ") || strings.Contains(lower, "delete ") {
		return true
	}
	// Colon-separated key-like values (e.g. "redis:user:42:session").
	if strings.Contains(lower, ":") && strings.ContainsAny(lower, "0123456789") {
		return true
	}
	return false
}

var (
	emailPattern     = regexp.MustCompile(`[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	privateKeyPattern = regexp.MustCompile(`-----begin (rsa |ec |)private key`)
	// phonePattern matches separated digit runs typical of phone numbers.
	phonePattern = regexp.MustCompile(`\+?[0-9][0-9\- ]{7,}`)
	// longDigitPattern matches bare ID/card-number runs (13+ digits).
	longDigitPattern = regexp.MustCompile(`[0-9]{13,}`)
)

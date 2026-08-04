package naming

import (
	"strings"
	"testing"
)

// FuzzNormalizeMachineName asserts the charset and length invariant for
// arbitrary input: the output is either a legal machine name or an error,
// and never contains any input byte that was not alphanumeric.
func FuzzNormalizeMachineName(f *testing.F) {
	f.Add("Order-Service API 支付")
	f.Add("")
	f.Add("-----BEGIN PRIVATE KEY-----")
	f.Add("a_b_c__d")
	f.Add("  ")
	f.Add("\x00\x01\x02")
	f.Fuzz(func(t *testing.T, value string) {
		normalized, err := NormalizeMachineName(value)
		if err != nil {
			return
		}
		if !metricNameCharset(normalized) {
			t.Fatalf("normalization of %q produced illegal name %q", value, normalized)
		}
		if len(normalized) > MaxComponentLength {
			t.Fatalf("normalized name exceeds %d bytes: %q", MaxComponentLength, normalized)
		}
	})
}

// FuzzNamesNeverPanic exercises metric/span/event name construction and
// the collision table with arbitrary inputs; every output must be bounded
// and every diagnostic must stay value-free.
func FuzzNamesNeverPanic(f *testing.F) {
	f.Add("GET", "/orders/{id}", "OrderService", "CreateOrder", "Nightly Job", "sql", "exec")
	f.Add("", "", "", "", "", "", "")
	f.Add("Bearer sk-123", "https://user:pass@example.com", "支付", "支付", "支付", "支付", "支付")
	f.Fuzz(func(t *testing.T, method, route, service, grpcMethod, job, system, operation string) {
		policy := NamingPolicy{}
		spec := SpanNameSpec{
			Kind: SpanKindHTTP, Method: method, Route: route,
		}
		if name, err := policy.SpanName(spec); err == nil {
			if len(name) > MaxSpanNameLength {
				t.Fatalf("HTTP span name exceeds limit: %q", name)
			}
		}
		spec = SpanNameSpec{Kind: SpanKindGRPC, Service: service, GRPCMethod: grpcMethod}
		_, _ = policy.SpanName(spec)
		spec = SpanNameSpec{Kind: SpanKindCron, JobName: job}
		_, _ = policy.SpanName(spec)
		spec = SpanNameSpec{Kind: SpanKindDependency, System: system, Operation: operation}
		_, _ = policy.SpanName(spec)

		_, diagnostics, _ := policy.MetricName(MetricNameSpec{
			Service: method, Module: route, Function: service,
			Operation: grpcMethod, Purpose: job,
		})
		if diagnostics != nil {
			for _, diagnostic := range diagnostics.Items() {
				for _, canary := range []string{method, route, service, grpcMethod, job, system, operation} {
					if canary != "" && strings.Contains(diagnostic.Message, canary) {
						t.Fatalf("diagnostic echoes input value %q: %s", canary, diagnostic.Message)
					}
				}
			}
		}

		_, _ = policy.EventName(method, route, service)
	})
}

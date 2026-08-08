package tracing

import (
	"context"
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// DependencyChildSpanPlanner plans one Child Span per dependency:
// PRODUCER/CONSUMER for Kafka, CLIENT for SQL/Redis/HTTP/RPC. Parents
// are always current_context — the static call graph is never treated as
// the unique runtime parent. It is stateless and safe for concurrent use.
type DependencyChildSpanPlanner struct{}

// childSpanSpec carries the per-kind constants of the Child Span mapping.
type childSpanSpec struct {
	// kind is the span kind.
	kind observabilityv1.SpanKind
	// system is the controlled system attribute value ("kafka", "sql",
	// "redis", "rpc"); empty for HTTP client.
	system string
	// nameSystem is the span name base, e.g. "kafka" or "db".
	nameSystem string
	// httpClient marks the HTTP client kind (method-based naming).
	httpClient bool
	// consumer marks the Kafka consumer call-site scope.
	consumer bool
}

// childSpanKinds is the exhaustive DependencyKind mapping. A kind absent
// from this table is unsupported and must be skipped with
// GEN_UNSUPPORTED_ENTITY — never guessed.
var childSpanKinds = map[observabilityv1.DependencyKind]childSpanSpec{
	observabilityv1.DependencyKind_KAFKA_PRODUCER: {
		kind: observabilityv1.SpanKind_SPAN_KIND_PRODUCER, system: "kafka", nameSystem: "kafka",
	},
	observabilityv1.DependencyKind_KAFKA_CONSUMER: {
		kind: observabilityv1.SpanKind_SPAN_KIND_CONSUMER, system: "kafka", nameSystem: "kafka", consumer: true,
	},
	observabilityv1.DependencyKind_SQL: {
		kind: observabilityv1.SpanKind_SPAN_KIND_CLIENT, system: "sql", nameSystem: "db",
	},
	observabilityv1.DependencyKind_REDIS: {
		kind: observabilityv1.SpanKind_SPAN_KIND_CLIENT, system: "redis", nameSystem: "redis",
	},
	observabilityv1.DependencyKind_HTTP_CLIENT: {
		kind: observabilityv1.SpanKind_SPAN_KIND_CLIENT, httpClient: true,
	},
	observabilityv1.DependencyKind_RPC_CLIENT: {
		kind: observabilityv1.SpanKind_SPAN_KIND_CLIENT, system: "rpc", nameSystem: "rpc",
	},
}

// PlanTracing implements planner.TracingPlanner for dependency child
// spans. Target values (URLs, SQL text, keys, topics, payloads,
// addresses) never enter names, attributes or diagnostics; dynamic
// targets degrade to generic spans with GEN_INCOMPLETE_TARGET.
func (DependencyChildSpanPlanner) PlanTracing(ctx context.Context, input *planner.SignalInput) (*planner.TracingResult, error) {
	result := &planner.TracingResult{}
	serviceName := input.Document.GetService().GetName()
	for _, dependency := range input.Document.Dependencies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec, supported := childSpanKinds[dependency.GetKind()]
		if !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalTracing,
				TargetID: dependency.GetId(), Field: "kind",
				Message: "dependency kind is not supported by the child span mapping",
			})
			result.Skipped++
			continue
		}
		span, diagnostics := buildChildSpan(dependency, input.Index, serviceName, spec)
		attachEvents(span, dependency.GetId(), input.Policy)
		result.Items = append(result.Items, span)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
	}

	disambiguateNames(result)

	sort.Slice(result.Items, func(left, right int) bool {
		return result.Items[left].GetId() < result.Items[right].GetId()
	})
	for _, span := range result.Items {
		sort.Slice(span.Attributes, func(left, right int) bool {
			return span.Attributes[left].GetKey() < span.Attributes[right].GetKey()
		})
		sort.Slice(span.Events, func(left, right int) bool {
			return span.Events[left].GetId() < span.Events[right].GetId()
		})
	}
	return result, nil
}

// buildChildSpan constructs one child span plan for the dependency.
func buildChildSpan(dependency *observabilityv1.Dependency, index *planner.Index, serviceName string, spec childSpanSpec) (*observabilityv1.SpanPlan, []naming.Diagnostic) {
	span := &observabilityv1.SpanPlan{
		Id:   planner.StableID(planner.SignalTracing, dependency.GetId(), planner.PurposeChild),
		Kind: spec.kind,
		Target: &observabilityv1.TargetRef{
			Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: dependency.GetId(),
		},
		StartTrigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_START},
		EndTrigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
		Parent: &observabilityv1.ParentStrategy{
			Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT,
		},
		Status: fullStatusMapping(),
	}

	var diagnostics []naming.Diagnostic
	emit := func(field, message string) {
		diagnostics = append(diagnostics, naming.Diagnostic{
			Code: policy.CodeIncompleteTarget, Signal: planner.SignalTracing,
			TargetID: dependency.GetId(), Field: field, Message: message,
		})
	}
	if !dependency.GetValueIsStatic() {
		emit("target", "dependency target is dynamic; target values are omitted")
	}

	span.Attributes = append(span.Attributes,
		serviceNameAttribute(serviceName),
		serviceVersionAttribute(),
	)
	owner := index.Function(dependency.GetFunctionId())
	if owner != nil {
		if owner.GetPackagePath() != "" {
			span.Attributes = append(span.Attributes, irStringAttribute("code.namespace", "function.package_path"))
		}
		if owner.GetQualifiedName() != "" {
			span.Attributes = append(span.Attributes, irStringAttribute("code.function", "function.qualified_name"))
		}
	}

	operation, operationErr := naming.NormalizeMachineName(dependency.GetOperation())
	if operationErr != nil {
		operation = "unknown"
		emit("operation", "dependency operation is missing; using the controlled fallback operation")
	}

	switch {
	case spec.httpClient:
		method := uppercase(dependency.GetOperation())
		switch method {
		case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		default:
			method = "HTTP"
			if operationErr != nil {
				emit("operation", "HTTP client method is missing; using the controlled fallback method")
			}
		}
		span.Name = "HTTP " + method
		span.Attributes = append(span.Attributes,
			constantStringAttribute("http.request.method", method))
	case spec.consumer:
		span.Name = "kafka " + operation
		span.Attributes = append(span.Attributes,
			constantStringAttribute("messaging.system", "kafka"),
			constantStringAttribute("messaging.operation", operation),
			constantStringAttribute("span.scope", "client_call"),
		)
	default:
		span.Name = fmt.Sprintf("%s %s", spec.nameSystem, operation)
		switch spec.system {
		case "kafka":
			span.Attributes = append(span.Attributes,
				constantStringAttribute("messaging.system", "kafka"),
				constantStringAttribute("messaging.operation", operation))
		case "sql", "redis":
			span.Attributes = append(span.Attributes,
				constantStringAttribute("db.system", spec.system),
				constantStringAttribute("db.operation", operation))
		case "rpc":
			span.Attributes = append(span.Attributes,
				constantStringAttribute("rpc.system", "rpc"),
				constantStringAttribute("operation", operation))
		}
	}
	return span, diagnostics
}

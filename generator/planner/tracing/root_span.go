package tracing

import (
	"context"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// OTelSemanticConventionsVersion is the pinned OpenTelemetry Semantic
// Conventions version used by every attribute mapping in this package.
// It is never "latest" and never a version range.
const OTelSemanticConventionsVersion = "1.37.0"

// EndpointRootSpanPlanner plans one Root Span per endpoint: SERVER for
// HTTP and gRPC, INTERNAL for Cron. It is stateless and safe for
// concurrent use. Plain functions and dependencies never get a root;
// Kafka consumer dependencies are left to the dependency span rules.
type EndpointRootSpanPlanner struct{}

// rootSpanSpec carries the per-kind constants of the Root Span mapping.
type rootSpanSpec struct {
	// kind is the span kind.
	kind observabilityv1.SpanKind
	// parent is the default parent strategy mode.
	parent observabilityv1.ParentStrategyMode
	// carrier is the abstract carrier for extract_or_root modes.
	carrier observabilityv1.CarrierType
	// rpcSystem is set for gRPC ("grpc").
	rpcSystem string
}

// rootSpanKinds is the exhaustive EndpointKind mapping. A kind absent
// from this table is unsupported and must be skipped with
// GEN_UNSUPPORTED_ENTITY — never guessed.
var rootSpanKinds = map[observabilityv1.EndpointKind]rootSpanSpec{
	observabilityv1.EndpointKind_HTTP_HANDLER: {
		kind: observabilityv1.SpanKind_SPAN_KIND_SERVER,
		parent: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT,
		carrier: observabilityv1.CarrierType_CARRIER_TYPE_HTTP_HEADERS,
	},
	observabilityv1.EndpointKind_GRPC_HANDLER: {
		kind: observabilityv1.SpanKind_SPAN_KIND_SERVER,
		parent: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT,
		carrier: observabilityv1.CarrierType_CARRIER_TYPE_GRPC_METADATA,
		rpcSystem: "grpc",
	},
	observabilityv1.EndpointKind_CRON_JOB: {
		kind: observabilityv1.SpanKind_SPAN_KIND_INTERNAL,
		parent: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT,
		carrier: observabilityv1.CarrierType_CARRIER_TYPE_NONE,
	},
}

// PlanTracing implements planner.TracingPlanner for endpoint root spans.
func (EndpointRootSpanPlanner) PlanTracing(ctx context.Context, input *planner.SignalInput) (*planner.TracingResult, error) {
	result := &planner.TracingResult{}
	serviceName := input.Document.GetService().GetName()
	for _, endpoint := range input.Document.Endpoints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec, supported := rootSpanKinds[endpoint.GetKind()]
		if !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalTracing,
				TargetID: endpoint.GetId(), Field: "kind",
				Message: "endpoint kind is not supported by the root span mapping",
			})
			result.Skipped++
			continue
		}
		span, diagnostics := buildRootSpan(endpoint, input.Index, serviceName, spec)
		attachEvents(span, endpoint.GetId(), input.Policy)
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

// buildRootSpan constructs one root span plan for the endpoint. Missing
// identity fields fall back to the stable function identity with
// GEN_INCOMPLETE_TARGET; the fallback never uses source paths.
func buildRootSpan(endpoint *observabilityv1.Endpoint, index *planner.Index, serviceName string, spec rootSpanSpec) (*observabilityv1.SpanPlan, []naming.Diagnostic) {
	span := &observabilityv1.SpanPlan{
		Id:   planner.StableID(planner.SignalTracing, endpoint.GetId(), planner.PurposeRoot),
		Kind: spec.kind,
		Target: &observabilityv1.TargetRef{
			Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId(),
		},
		StartTrigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_START},
		EndTrigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
		Parent: &observabilityv1.ParentStrategy{
			Mode: spec.parent, Carrier: spec.carrier,
		},
		Status: fullStatusMapping(),
	}

	var diagnostics []naming.Diagnostic
	emit := func(field, message string) {
		diagnostics = append(diagnostics, naming.Diagnostic{
			Code: policy.CodeIncompleteTarget, Signal: planner.SignalTracing,
			TargetID: endpoint.GetId(), Field: field, Message: message,
		})
	}

	span.Attributes = append(span.Attributes,
		serviceNameAttribute(serviceName),
		serviceVersionAttribute(),
	)
	function := index.Function(endpoint.GetFunctionId())
	if function != nil {
		if function.GetPackagePath() != "" {
			span.Attributes = append(span.Attributes, irStringAttribute("code.namespace", "function.package_path"))
		}
		if function.GetQualifiedName() != "" {
			span.Attributes = append(span.Attributes, irStringAttribute("code.function", "function.qualified_name"))
		}
	}

	name := ""
	switch endpoint.GetKind() {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		method := endpoint.GetHttpMethod()
		if method == "" || endpoint.GetHttpPath() == "" {
			emit("name", "http method or route is missing; using the function identity as the span name")
			name = functionIdentityName(function)
		} else {
			name = httpSpanName(method, endpoint.GetHttpPath())
		}
		span.Attributes = append(span.Attributes,
			constantStringAttribute("http.request.method", httpMethodConstant(method)),
			irStringAttribute("http.route", "endpoint.http_path"),
		)
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		if endpoint.GetGrpcService() == "" || endpoint.GetGrpcMethod() == "" {
			emit("name", "gRPC service or method is missing; using the function identity as the span name")
			name = functionIdentityName(function)
		} else {
			name = endpoint.GetGrpcService() + "/" + endpoint.GetGrpcMethod()
		}
		span.Attributes = append(span.Attributes,
			constantStringAttribute("rpc.system", spec.rpcSystem),
			irStringAttribute("rpc.service", "endpoint.grpc_service"),
			irStringAttribute("rpc.method", "endpoint.grpc_method"),
		)
	case observabilityv1.EndpointKind_CRON_JOB:
		if endpoint.GetName() == "" {
			emit("name", "cron job name is missing; using the function identity as the span name")
			name = functionIdentityName(function)
		} else {
			name = "cron " + endpoint.GetName()
		}
		span.Attributes = append(span.Attributes,
			irStringAttribute("cron.job.name", "endpoint.name"),
		)
		if endpoint.GetCronSchedule() != "" {
			span.Attributes = append(span.Attributes,
				irStringAttribute("cron.job.schedule", "endpoint.cron_schedule"))
		}
	}
	span.Name = name
	return span, diagnostics
}

// httpSpanName builds "<METHOD> <route-template>", falling back to the
// controlled "HTTP" prefix for unknown methods, per the P1-04 rules.
func httpSpanName(method, route string) string {
	method = uppercase(method)
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
	default:
		method = "HTTP"
	}
	return method + " " + route
}

// httpMethodConstant is the controlled HTTP method constant for the
// attribute binding; unknown methods degrade to "HTTP".
func httpMethodConstant(method string) string {
	method = uppercase(method)
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		return method
	default:
		return "HTTP"
	}
}

// functionIdentityName derives the stable function identity name; the
// final fallback is the controlled "endpoint" token, never a source
// path. The caller emits GEN_INCOMPLETE_TARGET.
func functionIdentityName(function *observabilityv1.Function) string {
	if function != nil && function.GetQualifiedName() != "" {
		return function.GetQualifiedName()
	}
	return "endpoint"
}

func fullStatusMapping() *observabilityv1.StatusPolicy {
	unset := observabilityv1.StatusSetting_STATUS_SETTING_UNSET
	errorStatus := observabilityv1.StatusSetting_STATUS_SETTING_ERROR
	return &observabilityv1.StatusPolicy{
		Ok: unset, Error: errorStatus, Timeout: errorStatus,
		Cancelled: errorStatus, Unknown: unset,
	}
}

func serviceNameAttribute(serviceName string) *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: "service.name",
		Value: &observabilityv1.ValueBinding{
			Path: "service.name", Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func serviceVersionAttribute() *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: "service.version",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.resource.service.version", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
			Fallback: "unknown",
		},
	}
}

func irStringAttribute(key, path string) *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: key,
		Value: &observabilityv1.ValueBinding{
			Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func constantStringAttribute(key, value string) *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: key,
		Value: &observabilityv1.ValueBinding{
			Path: "plan.constant." + value, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func uppercase(value string) string {
	upper := make([]byte, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		upper[index] = character
	}
	return string(upper)
}

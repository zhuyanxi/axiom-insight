package logging

import (
	"context"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// Field and value limits. Exceeding a limit produces a diagnostic; values
// are never silently truncated.
const (
	// MaxFieldsPerEvent bounds the generated fields of one event.
	MaxFieldsPerEvent = 20
	// MaxFieldNameLength bounds a generated field key.
	MaxFieldNameLength = 64
	// MaxConstantLength bounds a generated constant value.
	MaxConstantLength = 128
)

// RedactionPolicyID is the built-in redaction policy the Runtime applies
// to every log event; it is never inlined into the plan.
const RedactionPolicyID = "builtin:v1"

// LoggingPlanner plans structured log events for endpoints and
// dependencies. It is stateless and safe for concurrent use.
type LoggingPlanner struct{}

// endpointEventFamily carries the per-kind event family names.
type endpointEventFamily struct {
	started   string
	completed string
	failed    string
}

// endpointFamilies is the exhaustive EndpointKind mapping.
var endpointFamilies = map[observabilityv1.EndpointKind]endpointEventFamily{
	observabilityv1.EndpointKind_HTTP_HANDLER: {
		started: "http.request.started", completed: "http.request.completed", failed: "http.request.failed",
	},
	observabilityv1.EndpointKind_GRPC_HANDLER: {
		started: "rpc.request.started", completed: "rpc.request.completed", failed: "rpc.request.failed",
	},
	observabilityv1.EndpointKind_CRON_JOB: {
		started: "cron.job.started", completed: "cron.job.completed", failed: "cron.job.failed",
	},
}

// dependencyFailedEvent is the single dependency event family.
const dependencyFailedEvent = "dependency.operation.failed"

// failedStatuses is the documented status set that fires a failed event;
// completed events match ok only, so the two are mutually exclusive.
var failedStatuses = []observabilityv1.RuntimeStatus{
	observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
	observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT,
	observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED,
	observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN,
}

// PlanLogging implements planner.LoggingPlanner.
func (LoggingPlanner) PlanLogging(ctx context.Context, input *planner.SignalInput) (*planner.LoggingResult, error) {
	result := &planner.LoggingResult{}
	serviceName := input.Document.GetService().GetName()

	for _, endpoint := range input.Document.Endpoints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		family, supported := endpointFamilies[endpoint.GetKind()]
		if !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalLogging,
				TargetID: endpoint.GetId(), Field: "kind",
				Message: "endpoint kind is not supported by the logging event mapping",
			})
			result.Skipped++
			continue
		}
		operation, diagnostics := endpointOperation(endpoint)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		kindFields := endpointKindFields(endpoint)

		// Endpoints always have a provable Root Span: trace/span
		// correlation bindings are required; request_id stays optional.
		common := commonFields(serviceName, operation)
		if input.Policy.Logging.EmitStartEvents {
			event := baseEvent(endpoint.GetId(), family.started, planner.PurposeStart,
				observabilityv1.LogSeverity_LOG_SEVERITY_INFO,
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, endpoint.GetId(),
				observabilityv1.TriggerPhase_TRIGGER_PHASE_START, nil, common, kindFields)
			event.CorrelationFields = input.Policy.Logging.CorrelationFields
			event.Fields = append(event.Fields, correlationFields(input.Policy.Logging.CorrelationFields, true)...)
			event, dropped := finalizeEvent(event, input.Policy, endpoint.GetId())
			result.Diagnostics = append(result.Diagnostics, dropped...)
			result.Items = append(result.Items, event)
		}
		if input.Policy.Logging.EmitCompletionEvents {
			completed := baseEvent(endpoint.GetId(), family.completed, planner.PurposeEnd,
				severityForStatus(observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK),
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, endpoint.GetId(),
				observabilityv1.TriggerPhase_TRIGGER_PHASE_END,
				[]observabilityv1.RuntimeStatus{observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK},
				common, kindFields)
			completed.Fields = append(completed.Fields, statusField(), durationField())
			completed.CorrelationFields = input.Policy.Logging.CorrelationFields
			completed.Fields = append(completed.Fields, correlationFields(input.Policy.Logging.CorrelationFields, true)...)
			completed, dropped := finalizeEvent(completed, input.Policy, endpoint.GetId())
			result.Diagnostics = append(result.Diagnostics, dropped...)
			result.Items = append(result.Items, completed)

			failed := baseEvent(endpoint.GetId(), family.failed, planner.PurposeFailed,
				observabilityv1.LogSeverity_LOG_SEVERITY_ERROR,
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, endpoint.GetId(),
				observabilityv1.TriggerPhase_TRIGGER_PHASE_END, failedStatuses, common, kindFields)
			failed.Fields = append(failed.Fields, statusField(), durationField(), errorTypeField(), errorCategoryField())
			failed.CorrelationFields = input.Policy.Logging.CorrelationFields
			failed.Fields = append(failed.Fields, correlationFields(input.Policy.Logging.CorrelationFields, true)...)
			finalized, dropped := finalizeEvent(failed, input.Policy, endpoint.GetId())
			result.Diagnostics = append(result.Diagnostics, dropped...)
			result.Items = append(result.Items, finalized)
		}
	}

	for _, dependency := range input.Document.Dependencies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, supported := dependencyKindSupported(dependency.GetKind()); !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalLogging,
				TargetID: dependency.GetId(), Field: "kind",
				Message: "dependency kind is not supported by the logging event mapping",
			})
			result.Skipped++
			continue
		}
		if !input.Policy.Logging.EmitDependencyErrors {
			continue
		}
		operation, diagnostics := dependencyOperation(dependency)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		if !dependency.GetValueIsStatic() {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeIncompleteTarget, Signal: planner.SignalLogging,
				TargetID: dependency.GetId(), Field: "target",
				Message: "dependency target is dynamic; target values are omitted",
			})
		}
		// Dependencies have no provable Root Span context: trace/span
		// bindings are optional and omitted when absent (AC4).
		common := commonFields(serviceName, operation)
		common = append(common, constantField("system", operationSystem(dependency.GetKind())))
		event := baseEvent(dependency.GetId(), dependencyFailedEvent, planner.PurposeFailed,
			observabilityv1.LogSeverity_LOG_SEVERITY_ERROR,
			observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, dependency.GetId(),
			observabilityv1.TriggerPhase_TRIGGER_PHASE_END, failedStatuses, common, nil)
		event.Fields = append(event.Fields, statusField(), durationField(), errorTypeField(), errorCategoryField())
		event.CorrelationFields = input.Policy.Logging.CorrelationFields
		event.Fields = append(event.Fields, correlationFields(input.Policy.Logging.CorrelationFields, false)...)
		event, dropped := finalizeEvent(event, input.Policy, dependency.GetId())
		result.Diagnostics = append(result.Diagnostics, dropped...)
		result.Items = append(result.Items, event)
	}

	sort.Slice(result.Items, func(left, right int) bool {
		return result.Items[left].GetId() < result.Items[right].GetId()
	})
	for _, event := range result.Items {
		sort.Slice(event.Fields, func(left, right int) bool {
			return event.Fields[left].GetKey() < event.Fields[right].GetKey()
		})
	}
	return result, nil
}

// baseEvent builds one log plan with the common field set: timestamp
// (runtime clock), event name (plan constant), service/module/function/
// operation (IR constants), version (runtime resource, fallback unknown).
func baseEvent(targetID, eventName, purpose string, severity observabilityv1.LogSeverity, targetKind observabilityv1.TargetKind, target string, trigger observabilityv1.TriggerPhase, statuses []observabilityv1.RuntimeStatus, common []*observabilityv1.FieldBinding, kindFields []*observabilityv1.FieldBinding) *observabilityv1.LogPlan {
	fields := []*observabilityv1.FieldBinding{timestampField(), eventNameField(eventName)}
	fields = append(fields, common...)
	fields = append(fields, kindFields...)
	return &observabilityv1.LogPlan{
		Id:                planner.StableID(planner.SignalLogging, targetID, purpose),
		EventName:         eventName,
		Severity:          severity,
		Target:            &observabilityv1.TargetRef{Kind: targetKind, Id: target},
		Trigger:           &observabilityv1.Trigger{Phase: trigger},
		Statuses:          statuses,
		RedactionPolicyId: RedactionPolicyID,
		Fields:            fields,
	}
}

// commonFields are the fields shared by every event (timestamp and
// event.name are added by baseEvent).
func commonFields(serviceName, operation string) []*observabilityv1.FieldBinding {
	return []*observabilityv1.FieldBinding{
		irStringField("service", "service.name"),
		irStringField("module", "function.package_path"),
		irStringField("function", "function.qualified_name"),
		constantField("operation", operation),
		versionField(),
	}
}

// endpointKindFields adds the kind-specific controlled fields: HTTP
// method and route, gRPC service/method, cron stable job name.
func endpointKindFields(endpoint *observabilityv1.Endpoint) []*observabilityv1.FieldBinding {
	switch endpoint.GetKind() {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		return []*observabilityv1.FieldBinding{
			constantField("method", endpoint.GetHttpMethod()),
			irStringField("route", "endpoint.http_path"),
		}
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		return []*observabilityv1.FieldBinding{
			irStringField("rpc.service", "endpoint.grpc_service"),
			irStringField("rpc.method", "endpoint.grpc_method"),
		}
	default:
		return []*observabilityv1.FieldBinding{
			irStringField("cron.job.name", "endpoint.name"),
		}
	}
}

// correlationFields binds the configured correlation names as runtime
// context values: request_id is always optional, trace_id and span_id
// are required when a valid Root Span context exists (endpoints) and
// optional otherwise (dependencies). Fake or generated IDs never appear.
func correlationFields(names []string, traceRequired bool) []*observabilityv1.FieldBinding {
	fields := make([]*observabilityv1.FieldBinding, 0, len(names))
	for _, name := range names {
		switch name {
		case "request_id":
			fields = append(fields, runtimeContextField(name, false))
		case "trace_id", "span_id":
			fields = append(fields, runtimeContextField(name, traceRequired))
		}
	}
	return fields
}

func runtimeContextField(key string, required bool) *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: key,
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.context." + key, Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
			Required: required,
		},
	}
}

// finalizeEvent enforces the unified policy on every field: redaction
// (the built-in denylist cannot be disabled), field and constant limits,
// and duplicate normalized keys. Dropped fields produce value-free
// diagnostics; duplicate keys fail the whole plan.
func finalizeEvent(event *observabilityv1.LogPlan, resolved policy.Policy, targetID string) (*observabilityv1.LogPlan, []naming.Diagnostic) {
	attributes := naming.AttributePolicy{}
	var diagnostics []naming.Diagnostic
	seen := make(map[string]bool, len(event.Fields))
	kept := event.Fields[:0]
	for _, field := range event.Fields {
		if !attributes.LogFieldAllowed(field.GetKey(), resolved.Logging.RedactFields) {
			diagnostics = append(diagnostics, naming.Diagnostic{
				Code: policy.CodeSensitiveValueDropped, Signal: planner.SignalLogging,
				TargetID: targetID, Field: "fields." + field.GetKey(),
				Message: "field key is blocked by the redaction policy; the field is omitted",
			})
			continue
		}
		if len(field.GetKey()) > MaxFieldNameLength {
			diagnostics = append(diagnostics, naming.Diagnostic{
				Code: policy.CodeIncompleteTarget, Signal: planner.SignalLogging,
				TargetID: targetID, Field: "fields." + field.GetKey(),
				Message: "field name exceeds the length limit; the field is omitted",
			})
			continue
		}
		normalized := naming.NormalizeFieldKey(field.GetKey())
		if seen[normalized] {
			return nil, []naming.Diagnostic{{
				Code: policy.CodeInvalidIR, Signal: planner.SignalLogging,
				TargetID: targetID, Field: "fields." + field.GetKey(),
				Message: "duplicate field key after normalization",
			}}
		}
		seen[normalized] = true
		if constant := constantValue(field.GetValue()); constant != "" {
			if naming.IsSensitiveValue(constant) || naming.IsHighCardinalityValue(constant) {
				diagnostics = append(diagnostics, naming.Diagnostic{
					Code: policy.CodeSensitiveValueDropped, Signal: planner.SignalLogging,
					TargetID: targetID, Field: "fields." + field.GetKey(),
					Message: "constant value is blocked by the sensitive-value policy; the field is omitted",
				})
				continue
			}
			if len(constant) > MaxConstantLength {
				diagnostics = append(diagnostics, naming.Diagnostic{
					Code: policy.CodeIncompleteTarget, Signal: planner.SignalLogging,
					TargetID: targetID, Field: "fields." + field.GetKey(),
					Message: "constant value exceeds the length limit; the field is omitted",
				})
				continue
			}
		}
		kept = append(kept, field)
	}
	if len(kept) > MaxFieldsPerEvent {
		diagnostics = append(diagnostics, naming.Diagnostic{
			Code: policy.CodeCardinalityBlocked, Signal: planner.SignalLogging,
			TargetID: targetID, Field: "fields",
			Message: "event exceeds the maximum field count; excess fields are omitted",
		})
		kept = kept[:MaxFieldsPerEvent]
	}
	event.Fields = kept
	return event, diagnostics
}

// constantValue returns the constant string carried by a plan constant
// binding, or "" for non-constant bindings.
func constantValue(binding *observabilityv1.ValueBinding) string {
	if binding == nil || binding.GetSource() != observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT {
		return ""
	}
	path := binding.GetPath()
	const prefix = "plan.constant."
	if len(path) <= len(prefix) {
		return ""
	}
	return path[len(prefix):]
}

func timestampField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "timestamp",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.clock", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CLOCK,
			Type: observabilityv1.ValueType_VALUE_TYPE_TIMESTAMP, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func eventNameField(eventName string) *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "event.name",
		Value: &observabilityv1.ValueBinding{
			Path: "plan.constant." + eventName, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func irStringField(key, path string) *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: key,
		Value: &observabilityv1.ValueBinding{
			Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func constantField(key, value string) *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: key,
		Value: &observabilityv1.ValueBinding{
			Path: "plan.constant." + value, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

// versionField binds the runtime resource version with the documented
// "unknown" fallback; never a fake value.
func versionField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "version",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.resource.service.version", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
			Fallback: "unknown",
		},
	}
}

func statusField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "status",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.operation.status", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STATUS, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		},
	}
}

func durationField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "duration_seconds",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.operation.duration_seconds", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type: observabilityv1.ValueType_VALUE_TYPE_DOUBLE, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		},
	}
}

// errorTypeField binds only the safe error type; error.message and
// stacktraces are never bound.
func errorTypeField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "error.type",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.error.type", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		},
	}
}

// errorCategoryField binds the controlled error category derived from the
// runtime status; raw error strings never become categories.
func errorCategoryField() *observabilityv1.FieldBinding {
	return &observabilityv1.FieldBinding{
		Key: "error.category",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.error.category", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		},
	}
}

// severityForStatus maps each runtime status to one documented severity.
func severityForStatus(status observabilityv1.RuntimeStatus) observabilityv1.LogSeverity {
	switch status {
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK:
		return observabilityv1.LogSeverity_LOG_SEVERITY_INFO
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN:
		return observabilityv1.LogSeverity_LOG_SEVERITY_WARN
	default:
		return observabilityv1.LogSeverity_LOG_SEVERITY_ERROR
	}
}

// endpointOperation derives the controlled operation per endpoint kind.
func endpointOperation(endpoint *observabilityv1.Endpoint) (string, []naming.Diagnostic) {
	switch endpoint.GetKind() {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		operation, err := naming.NormalizeMachineName(endpoint.GetHttpMethod())
		if err != nil || operation == "" {
			return "http", incompleteTarget(endpoint.GetId(), "operation", "endpoint method is missing; using a controlled fallback operation")
		}
		return operation, nil
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		if endpoint.GetGrpcService() != "" && endpoint.GetGrpcMethod() != "" {
			operation, err := naming.NormalizeMachineName(endpoint.GetGrpcService() + "/" + endpoint.GetGrpcMethod())
			if err == nil {
				return operation, nil
			}
		}
		return "grpc", incompleteTarget(endpoint.GetId(), "operation", "gRPC service or method is missing; using a controlled fallback operation")
	default:
		operation, err := naming.NormalizeMachineName(endpoint.GetName())
		if err != nil || operation == "" {
			return "cron", incompleteTarget(endpoint.GetId(), "operation", "cron job name is missing; using a controlled fallback operation")
		}
		return operation, nil
	}
}

func dependencyOperation(dependency *observabilityv1.Dependency) (string, []naming.Diagnostic) {
	operation, err := naming.NormalizeMachineName(dependency.GetOperation())
	if err != nil || operation == "" {
		return "unknown", incompleteTarget(dependency.GetId(), "operation", "dependency operation is missing; using the controlled fallback operation")
	}
	return operation, nil
}

func incompleteTarget(targetID, field, message string) []naming.Diagnostic {
	return []naming.Diagnostic{{
		Code: policy.CodeIncompleteTarget, Signal: planner.SignalLogging,
		TargetID: targetID, Field: field, Message: message,
	}}
}

// dependencyKindSupported reports whether a dependency kind has a logging
// event mapping (all six kinds do).
func dependencyKindSupported(kind observabilityv1.DependencyKind) (bool, bool) {
	switch kind {
	case observabilityv1.DependencyKind_KAFKA_PRODUCER,
		observabilityv1.DependencyKind_KAFKA_CONSUMER,
		observabilityv1.DependencyKind_SQL,
		observabilityv1.DependencyKind_REDIS,
		observabilityv1.DependencyKind_HTTP_CLIENT,
		observabilityv1.DependencyKind_RPC_CLIENT:
		return true, true
	default:
		return false, false
	}
}

// operationSystem derives the controlled system constant for a dependency
// kind, matching the metrics vocabulary.
func operationSystem(kind observabilityv1.DependencyKind) string {
	switch kind {
	case observabilityv1.DependencyKind_KAFKA_PRODUCER, observabilityv1.DependencyKind_KAFKA_CONSUMER:
		return "kafka"
	case observabilityv1.DependencyKind_SQL:
		return "sql"
	case observabilityv1.DependencyKind_REDIS:
		return "redis"
	case observabilityv1.DependencyKind_HTTP_CLIENT:
		return "http"
	default:
		return "rpc"
	}
}

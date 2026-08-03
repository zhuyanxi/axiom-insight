package observabilityv1

import (
	"fmt"
)

// GenerationPlanSchemaVersion is the current GenerationPlan contract
// version. It evolves independently from the IR schema version recorded in
// GenerationPlan.SourceIrSchemaVersion.
const GenerationPlanSchemaVersion = "v1"

// PlanValidationError describes one violation found by
// ValidateGenerationPlan. Field is a dotted path into the plan (for example
// "metrics[2].target.id"); EntityID identifies the offending plan item or
// referenced IR entity and is empty when the violation has no entity.
type PlanValidationError struct {
	Field    string
	EntityID string
	Message  string
}

// Error implements error and keeps the violation self-contained.
func (violation *PlanValidationError) Error() string {
	subject := violation.Field
	if violation.EntityID != "" {
		subject += fmt.Sprintf(" (entity %q)", violation.EntityID)
	}
	return fmt.Sprintf("%s: %s", subject, violation.Message)
}

// ValidateGenerationPlan checks the structural integrity of plan against
// document. It reports every violation in deterministic order; the result
// slice is fresh on every call and never aliases caller state. Validation
// is read-only: neither input is modified. Complexity is O(M+S+L) plus one
// index pass over the document entities.
//
// A nil result means the plan is valid. A nil plan or nil document is a
// violation, not a panic.
func ValidateGenerationPlan(document *ObservabilityDocument, plan *GenerationPlan) []*PlanValidationError {
	if plan == nil {
		return []*PlanValidationError{{
			Field:   "generation_plan",
			Message: "plan is nil",
		}}
	}
	if document == nil {
		return []*PlanValidationError{{
			Field:   "generation_plan",
			Message: "document is nil; cannot validate target references",
		}}
	}

	var violations []*PlanValidationError
	emit := func(violation *PlanValidationError) {
		violations = append(violations, violation)
	}

	if plan.SchemaVersion == "" {
		emit(&PlanValidationError{Field: "schema_version", Message: "schema version is empty"})
	}
	if plan.ServiceName == "" {
		emit(&PlanValidationError{Field: "service_name", Message: "service name is empty"})
	}

	index := newEntityIndex(document)
	seen := make(map[string]string, len(plan.Metrics)+len(plan.Spans)+len(plan.Logs))
	register := func(id, field string) {
		if id == "" {
			emit(&PlanValidationError{Field: field, Message: "plan ID is empty"})
			return
		}
		if previous, ok := seen[id]; ok {
			emit(&PlanValidationError{
				Field:    field,
				EntityID: id,
				Message:  fmt.Sprintf("duplicate plan ID; first declared at %s", previous),
			})
			return
		}
		seen[id] = field
	}

	for i, metric := range plan.Metrics {
		field := fmt.Sprintf("metrics[%d]", i)
		register(metric.GetId(), field+".id")
		if metric.GetName() == "" {
			emit(&PlanValidationError{Field: field + ".name", EntityID: metric.GetId(), Message: "metric name is empty"})
		}
		if metric.GetType() == MetricType_METRIC_TYPE_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".type", EntityID: metric.GetId(), Message: "metric type is unspecified"})
		}
		validateTarget(emit, index, metric.GetId(), metric.GetTarget(), field+".target")
		if metric.GetTrigger() == nil {
			emit(&PlanValidationError{Field: field + ".trigger", EntityID: metric.GetId(), Message: "record trigger is missing"})
		} else if metric.GetTrigger().GetPhase() == TriggerPhase_TRIGGER_PHASE_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".trigger.phase", EntityID: metric.GetId(), Message: "trigger phase is unspecified"})
		}
		validateValueBinding(emit, metric.GetId(), metric.GetValue(), field+".value")
		for j, attribute := range metric.GetAttributes() {
			validateAttributeBinding(emit, metric.GetId(), attribute, fmt.Sprintf("%s.attributes[%d]", field, j))
		}
	}

	for i, span := range plan.Spans {
		field := fmt.Sprintf("spans[%d]", i)
		register(span.GetId(), field+".id")
		if span.GetName() == "" {
			emit(&PlanValidationError{Field: field + ".name", EntityID: span.GetId(), Message: "span name is empty"})
		}
		if span.GetKind() == SpanKind_SPAN_KIND_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".kind", EntityID: span.GetId(), Message: "span kind is unspecified"})
		}
		validateTarget(emit, index, span.GetId(), span.GetTarget(), field+".target")
		if span.GetStartTrigger() == nil {
			emit(&PlanValidationError{Field: field + ".start_trigger", EntityID: span.GetId(), Message: "start trigger is missing"})
		} else if span.GetStartTrigger().GetPhase() == TriggerPhase_TRIGGER_PHASE_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".start_trigger.phase", EntityID: span.GetId(), Message: "start trigger phase is unspecified"})
		}
		if span.GetEndTrigger() == nil {
			emit(&PlanValidationError{Field: field + ".end_trigger", EntityID: span.GetId(), Message: "end trigger is missing"})
		} else if span.GetEndTrigger().GetPhase() == TriggerPhase_TRIGGER_PHASE_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".end_trigger.phase", EntityID: span.GetId(), Message: "end trigger phase is unspecified"})
		}
		if span.GetParent() == nil {
			emit(&PlanValidationError{Field: field + ".parent", EntityID: span.GetId(), Message: "parent strategy is missing"})
		} else {
			validateParentStrategy(emit, span, fmt.Sprintf("%s.parent", field))
		}
		validateStatusPolicy(emit, span.GetId(), span.GetStatus(), field+".status")
		for j, event := range span.GetEvents() {
			validateSpanEvent(emit, span.GetId(), event, fmt.Sprintf("%s.events[%d]", field, j))
		}
		for j, attribute := range span.GetAttributes() {
			validateAttributeBinding(emit, span.GetId(), attribute, fmt.Sprintf("%s.attributes[%d]", field, j))
		}
	}

	for i, log := range plan.Logs {
		field := fmt.Sprintf("logs[%d]", i)
		register(log.GetId(), field+".id")
		if log.GetEventName() == "" {
			emit(&PlanValidationError{Field: field + ".event_name", EntityID: log.GetId(), Message: "event name is empty"})
		}
		if log.GetSeverity() == LogSeverity_LOG_SEVERITY_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".severity", EntityID: log.GetId(), Message: "severity is unspecified"})
		}
		validateTarget(emit, index, log.GetId(), log.GetTarget(), field+".target")
		if log.GetTrigger() == nil {
			emit(&PlanValidationError{Field: field + ".trigger", EntityID: log.GetId(), Message: "trigger is missing"})
		} else if log.GetTrigger().GetPhase() == TriggerPhase_TRIGGER_PHASE_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".trigger.phase", EntityID: log.GetId(), Message: "trigger phase is unspecified"})
		}
		for j, fieldBinding := range log.GetFields() {
			validateFieldBinding(emit, log.GetId(), fieldBinding, fmt.Sprintf("%s.fields[%d]", field, j))
		}
		for j, correlation := range log.GetCorrelationFields() {
			if correlation == "" {
				emit(&PlanValidationError{Field: fmt.Sprintf("%s.correlation_fields[%d]", field, j), EntityID: log.GetId(), Message: "correlation field name is empty"})
			}
		}
	}

	for i, diagnostic := range plan.Diagnostics {
		field := fmt.Sprintf("diagnostics[%d]", i)
		if diagnostic.GetSeverity() == PlanSeverity_PLAN_SEVERITY_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + ".severity", Message: "diagnostic severity is unspecified"})
		}
		if diagnostic.GetCode() == "" {
			emit(&PlanValidationError{Field: field + ".code", Message: "diagnostic code is empty"})
		}
	}

	return violations
}

// validateTarget checks the target reference against the document entity
// index: kind must be specified, ID must be non-empty, the ID must exist
// and the entity type must match the declared kind.
func validateTarget(emit func(*PlanValidationError), index *entityIndex, ownerID string, target *TargetRef, field string) {
	if target == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "target reference is missing"})
		return
	}
	if target.GetKind() == TargetKind_TARGET_KIND_UNSPECIFIED {
		emit(&PlanValidationError{Field: field + ".kind", EntityID: ownerID, Message: "target kind is unspecified"})
	}
	if target.GetId() == "" {
		emit(&PlanValidationError{Field: field + ".id", EntityID: ownerID, Message: "target ID is empty"})
		return
	}
	actual, ok := index.lookup(target.GetId())
	if !ok {
		emit(&PlanValidationError{
			Field:    field + ".id",
			EntityID: target.GetId(),
			Message:  "target ID does not exist in the document",
		})
		return
	}
	if actual != target.GetKind() {
		emit(&PlanValidationError{
			Field:    field + ".kind",
			EntityID: target.GetId(),
			Message:  fmt.Sprintf("target kind %s does not match entity type %s", target.GetKind(), actual),
		})
	}
}

func validateValueBinding(emit func(*PlanValidationError), ownerID string, binding *ValueBinding, field string) {
	if binding == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "value binding is missing"})
		return
	}
	if binding.GetSource() == ValueSource_VALUE_SOURCE_UNSPECIFIED {
		emit(&PlanValidationError{Field: field + ".source", EntityID: ownerID, Message: "value source is unspecified"})
	}
	if binding.GetType() == ValueType_VALUE_TYPE_UNSPECIFIED {
		emit(&PlanValidationError{Field: field + ".type", EntityID: ownerID, Message: "value type is unspecified"})
	}
	if binding.GetCardinality() == CardinalityClass_CARDINALITY_CLASS_UNSPECIFIED {
		emit(&PlanValidationError{Field: field + ".cardinality", EntityID: ownerID, Message: "cardinality class is unspecified"})
	}
	if binding.GetPath() == "" {
		emit(&PlanValidationError{Field: field + ".path", EntityID: ownerID, Message: "value path is empty"})
	}
}

func validateAttributeBinding(emit func(*PlanValidationError), ownerID string, binding *AttributeBinding, field string) {
	if binding == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "attribute binding is missing"})
		return
	}
	if binding.GetKey() == "" {
		emit(&PlanValidationError{Field: field + ".key", EntityID: ownerID, Message: "attribute key is empty"})
	}
	validateValueBinding(emit, ownerID, binding.GetValue(), field+".value")
}

func validateFieldBinding(emit func(*PlanValidationError), ownerID string, binding *FieldBinding, field string) {
	if binding == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "field binding is missing"})
		return
	}
	if binding.GetKey() == "" {
		emit(&PlanValidationError{Field: field + ".key", EntityID: ownerID, Message: "field key is empty"})
	}
	validateValueBinding(emit, ownerID, binding.GetValue(), field+".value")
}

func validateParentStrategy(emit func(*PlanValidationError), span *SpanPlan, field string) {
	mode := span.GetParent().GetMode()
	if mode == ParentStrategyMode_PARENT_STRATEGY_MODE_UNSPECIFIED {
		emit(&PlanValidationError{Field: field + ".mode", EntityID: span.GetId(), Message: "parent strategy mode is unspecified"})
	}
	staticParent := span.GetParent().GetStaticParentSpanId()
	if mode == ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC && staticParent == "" {
		emit(&PlanValidationError{Field: field + ".static_parent_span_id", EntityID: span.GetId(), Message: "static parent mode requires a static parent span ID"})
	}
	if mode != ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC && staticParent != "" {
		emit(&PlanValidationError{Field: field + ".static_parent_span_id", EntityID: span.GetId(), Message: "static parent span ID set outside static parent mode"})
	}
}

// validateStatusPolicy requires the policy to be present and every runtime
// status to have an explicit, documented mapping (no default fallthrough).
func validateStatusPolicy(emit func(*PlanValidationError), ownerID string, policy *StatusPolicy, field string) {
	if policy == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "status policy is missing"})
		return
	}
	// Fixed iteration order keeps the violation list deterministic.
	for _, entry := range []struct {
		name    string
		setting StatusSetting
	}{
		{"ok", policy.GetOk()},
		{"error", policy.GetError()},
		{"timeout", policy.GetTimeout()},
		{"cancelled", policy.GetCancelled()},
		{"unknown", policy.GetUnknown()},
	} {
		if entry.setting == StatusSetting_STATUS_SETTING_UNSPECIFIED {
			emit(&PlanValidationError{Field: field + "." + entry.name, EntityID: ownerID, Message: "runtime status mapping is unspecified"})
		}
	}
}

func validateSpanEvent(emit func(*PlanValidationError), ownerID string, event *SpanEvent, field string) {
	if event == nil {
		emit(&PlanValidationError{Field: field, EntityID: ownerID, Message: "span event is missing"})
		return
	}
	if event.GetId() == "" {
		emit(&PlanValidationError{Field: field + ".id", EntityID: ownerID, Message: "event ID is empty"})
	}
	if event.GetName() == "" {
		emit(&PlanValidationError{Field: field + ".name", EntityID: ownerID, Message: "event name is empty"})
	}
	for j, status := range event.GetStatuses() {
		if status == RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED {
			emit(&PlanValidationError{Field: fmt.Sprintf("%s.statuses[%d]", field, j), EntityID: ownerID, Message: "event status is unspecified"})
		}
	}
	for j, attribute := range event.GetAttributes() {
		validateAttributeBinding(emit, ownerID, attribute, fmt.Sprintf("%s.attributes[%d]", field, j))
	}
}

// entityIndex maps every IR entity ID to its target kind. It is built in
// one pass and never retains slices from the document.
type entityIndex struct {
	kinds map[string]TargetKind
}

func newEntityIndex(document *ObservabilityDocument) *entityIndex {
	index := &entityIndex{kinds: make(map[string]TargetKind)}
	for _, endpoint := range document.GetEndpoints() {
		index.kinds[endpoint.GetId()] = TargetKind_TARGET_KIND_ENDPOINT
	}
	for _, function := range document.GetFunctions() {
		index.kinds[function.GetId()] = TargetKind_TARGET_KIND_FUNCTION
	}
	for _, dependency := range document.GetDependencies() {
		index.kinds[dependency.GetId()] = TargetKind_TARGET_KIND_DEPENDENCY
	}
	for _, edge := range document.GetCallEdges() {
		index.kinds[edge.GetId()] = TargetKind_TARGET_KIND_CALL_EDGE
	}
	return index
}

func (index *entityIndex) lookup(id string) (TargetKind, bool) {
	kind, ok := index.kinds[id]
	return kind, ok
}

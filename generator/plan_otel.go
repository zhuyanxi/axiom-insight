package generator

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// RenderTracingPlan converts the spans section of a validated
// GenerationPlan into the closed otel.yaml contract and renders it to
// canonical bytes. The file is explicitly an instrumentation plan —
// plan_kind is "instrumentation" — never an OpenTelemetry Collector
// configuration; the renderer can never add sampling, exporter, endpoint
// or credential fields. The rendered bytes are immediately re-parsed with
// the strict decoder and re-validated semantically.
//
// Errors carry the GEN_RENDER_ERROR context and the offending span ID; no
// partial bytes are ever returned. The function touches no filesystem,
// environment, clock or network.
func RenderTracingPlan(plan *observabilityv1.GenerationPlan, _ policy.Policy) ([]byte, error) {
	document, err := otelDocument(plan)
	if err != nil {
		return nil, err
	}
	contents, err := RenderOTel(document)
	if err != nil {
		return nil, renderFailure("", err)
	}
	if _, err := DecodeOTel(contents); err != nil {
		return nil, renderFailure("", err)
	}
	return contents, nil
}

// otelDocument converts the plan into the typed otel.yaml model.
func otelDocument(plan *observabilityv1.GenerationPlan) (*OTelDocument, error) {
	if plan == nil {
		return nil, renderFailure("$", fmt.Errorf("plan is nil"))
	}
	if plan.GetSchemaVersion() != observabilityv1.GenerationPlanSchemaVersion {
		return nil, renderFailure("$", fmt.Errorf(
			"plan schema version %q is not supported; supported: %s",
			plan.GetSchemaVersion(), observabilityv1.GenerationPlanSchemaVersion))
	}
	document := &OTelDocument{
		SchemaVersion:              SchemaVersionOTel,
		DocumentType:               DocumentTypeOTel,
		PlanKind:                   OTelPlanKind,
		SemanticConventionsVersion: OTelSemanticConventionsVersion,
		Source: Source{
			IRSchemaVersion: plan.GetSourceIrSchemaVersion(),
			ServiceName:     plan.GetServiceName(),
		},
		GeneratedBy: GeneratedBy{Name: GeneratorName, Version: GeneratorVersion},
	}

	spanIDs := make(map[string]bool, len(plan.GetSpans()))
	for _, spanPlan := range plan.GetSpans() {
		if spanPlan.GetId() == "" {
			return nil, renderFailure("$", fmt.Errorf("span id is empty"))
		}
		spanIDs[spanPlan.GetId()] = true
	}
	for _, spanPlan := range plan.GetSpans() {
		converted, err := convertSpan(spanPlan, spanIDs)
		if err != nil {
			return nil, renderFailure(spanPlan.GetId(), err)
		}
		document.Spans = append(document.Spans, converted)
	}

	document.Resources = planResources(plan)
	return document, nil
}

// planResources collects the document-level resource attributes
// (service.name, service.version) from the plan spans. They are rendered
// once at the document level and never duplicated per span; convertSpan
// skips them.
func planResources(plan *observabilityv1.GenerationPlan) []Attribute {
	var resources []Attribute
	seen := make(map[string]bool)
	for _, span := range plan.GetSpans() {
		for _, attribute := range span.GetAttributes() {
			if attribute.GetKey() != "service.name" && attribute.GetKey() != "service.version" {
				continue
			}
			if seen[attribute.GetKey()] {
				continue
			}
			seen[attribute.GetKey()] = true
			binding, err := convertValueBinding(attribute.GetValue())
			if err != nil {
				continue
			}
			resources = append(resources, Attribute{
				Key:     attribute.GetKey(),
				Type:    bindingTypeName(attribute.GetValue().GetType()),
				Binding: binding,
			})
		}
	}
	sort.Slice(resources, func(left, right int) bool {
		return resources[left].Key < resources[right].Key
	})
	return resources
}

func convertSpan(span *observabilityv1.SpanPlan, spanIDs map[string]bool) (Span, error) {
	if span.GetId() == "" {
		return Span{}, fmt.Errorf("span id is empty")
	}
	if span.GetName() == "" {
		return Span{}, fmt.Errorf("span name is empty")
	}
	kind, err := spanKindName(span.GetKind())
	if err != nil {
		return Span{}, err
	}
	target, err := convertTarget(span.GetTarget())
	if err != nil {
		return Span{}, err
	}
	start, err := triggerPhaseName(span.GetStartTrigger().GetPhase())
	if err != nil {
		return Span{}, err
	}
	end, err := triggerPhaseName(span.GetEndTrigger().GetPhase())
	if err != nil {
		return Span{}, err
	}
	parent, err := convertParent(span, spanIDs)
	if err != nil {
		return Span{}, err
	}
	status, err := convertStatus(span.GetStatus())
	if err != nil {
		return Span{}, err
	}

	converted := Span{
		ID:        span.GetId(),
		Name:      span.GetName(),
		Kind:      kind,
		Target:    target,
		Lifecycle: Lifecycle{Start: start, End: end},
		Parent:    parent,
		Status:    status,
	}

	// Resource attributes (service.name, service.version) are grouped at
	// the document level, never duplicated per span.
	for _, attribute := range span.GetAttributes() {
		if attribute.GetKey() == "service.name" || attribute.GetKey() == "service.version" {
			continue
		}
		binding, err := convertValueBinding(attribute.GetValue())
		if err != nil {
			return Span{}, fmt.Errorf("attribute %q: %w", attribute.GetKey(), err)
		}
		converted.Attributes = append(converted.Attributes, Attribute{
			Key:     attribute.GetKey(),
			Type:    bindingTypeName(attribute.GetValue().GetType()),
			Binding: binding,
		})
	}

	for _, event := range span.GetEvents() {
		convertedEvent, err := convertSpanEvent(event)
		if err != nil {
			return Span{}, err
		}
		converted.Events = append(converted.Events, convertedEvent)
	}
	return converted, nil
}

// convertParent maps the plan parent strategy and carrier onto the YAML
// contract and validates kind/mode combinations: endpoint roots may use
// extract_or_root or new_root, dependency and call-edge spans must use
// current_context, and a static parent must reference an existing span.
func convertParent(span *observabilityv1.SpanPlan, spanIDs map[string]bool) (Parent, error) {
	mode, err := parentModeName(span.GetParent().GetMode())
	if err != nil {
		return Parent{}, err
	}
	parent := Parent{Strategy: mode}
	switch mode {
	case ParentExtractOrRoot:
		carrier, err := carrierName(span.GetParent().GetCarrier())
		if err != nil {
			return Parent{}, err
		}
		if carrier == CarrierNone {
			return Parent{}, fmt.Errorf("extract_or_root requires a real carrier, got none")
		}
		parent.Carrier = carrier
	case ParentNewRoot:
		// Roots (endpoints) may start new roots; dependency and internal
		// spans must follow the current context.
		switch span.GetTarget().GetKind() {
		case observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY,
			observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE:
			return Parent{}, fmt.Errorf("dependency span must use current_context, got new_root")
		}
	case ParentCurrentContext:
	case ParentStatic:
		staticParent := span.GetParent().GetStaticParentSpanId()
		if staticParent == "" {
			return Parent{}, fmt.Errorf("static parent requires a static_parent_span_id")
		}
		if !spanIDs[staticParent] {
			return Parent{}, fmt.Errorf("static parent references unknown span %q", staticParent)
		}
		parent.StaticParentSpanID = staticParent
	default:
		return Parent{}, fmt.Errorf("unsupported parent strategy %v", span.GetParent().GetMode())
	}
	return parent, nil
}

// convertStatus copies the plan's complete status mapping verbatim; an
// absent or incomplete mapping is a render error.
func convertStatus(status *observabilityv1.StatusPolicy) (*StatusPolicy, error) {
	if status == nil {
		return nil, fmt.Errorf("status policy is missing")
	}
	mapping := make(map[string]string, 5)
	entries := []struct {
		status observabilityv1.RuntimeStatus
		key    string
	}{
		{observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK, RuntimeStatusOK},
		{observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR, RuntimeStatusError},
		{observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT, RuntimeStatusTimeout},
		{observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED, RuntimeStatusCancelled},
		{observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN, RuntimeStatusUnknown},
	}
	for _, entry := range entries {
		setting, err := statusSettingName(statusFor(entry.status, status))
		if err != nil {
			return nil, fmt.Errorf("status mapping for %s: %w", entry.key, err)
		}
		mapping[entry.key] = setting
	}
	return &StatusPolicy{Mapping: mapping}, nil
}

func statusFor(runtimeStatus observabilityv1.RuntimeStatus, status *observabilityv1.StatusPolicy) observabilityv1.StatusSetting {
	switch runtimeStatus {
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK:
		return status.GetOk()
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR:
		return status.GetError()
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT:
		return status.GetTimeout()
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED:
		return status.GetCancelled()
	default:
		return status.GetUnknown()
	}
}

func convertSpanEvent(event *observabilityv1.SpanEvent) (SpanEvent, error) {
	if event.GetId() == "" {
		return SpanEvent{}, fmt.Errorf("event id is empty")
	}
	if event.GetName() == "" {
		return SpanEvent{}, fmt.Errorf("event name is empty")
	}
	converted := SpanEvent{ID: event.GetId(), Name: event.GetName()}
	for _, runtimeStatus := range event.GetStatuses() {
		name, err := runtimeStatusName(runtimeStatus)
		if err != nil {
			return SpanEvent{}, fmt.Errorf("event %s: %w", event.GetId(), err)
		}
		converted.Statuses = append(converted.Statuses, name)
	}
	for _, attribute := range event.GetAttributes() {
		binding, err := convertValueBinding(attribute.GetValue())
		if err != nil {
			return SpanEvent{}, fmt.Errorf("event %s attribute %q: %w", event.GetId(), attribute.GetKey(), err)
		}
		converted.Attributes = append(converted.Attributes, Attribute{
			Key:     attribute.GetKey(),
			Type:    bindingTypeName(attribute.GetValue().GetType()),
			Binding: binding,
		})
	}
	return converted, nil
}

func spanKindName(kind observabilityv1.SpanKind) (string, error) {
	switch kind {
	case observabilityv1.SpanKind_SPAN_KIND_SERVER:
		return SpanKindServer, nil
	case observabilityv1.SpanKind_SPAN_KIND_CLIENT:
		return SpanKindClient, nil
	case observabilityv1.SpanKind_SPAN_KIND_PRODUCER:
		return SpanKindProducer, nil
	case observabilityv1.SpanKind_SPAN_KIND_CONSUMER:
		return SpanKindConsumer, nil
	case observabilityv1.SpanKind_SPAN_KIND_INTERNAL:
		return SpanKindInternal, nil
	default:
		return "", fmt.Errorf("unsupported span kind %v", kind)
	}
}

func parentModeName(mode observabilityv1.ParentStrategyMode) (string, error) {
	switch mode {
	case observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT:
		return ParentExtractOrRoot, nil
	case observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT:
		return ParentNewRoot, nil
	case observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT:
		return ParentCurrentContext, nil
	case observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC:
		return ParentStatic, nil
	default:
		return "", fmt.Errorf("unsupported parent strategy %v", mode)
	}
}

func carrierName(carrier observabilityv1.CarrierType) (string, error) {
	switch carrier {
	case observabilityv1.CarrierType_CARRIER_TYPE_HTTP_HEADERS:
		return CarrierHTTPHeaders, nil
	case observabilityv1.CarrierType_CARRIER_TYPE_GRPC_METADATA:
		return CarrierGRPCMetadata, nil
	case observabilityv1.CarrierType_CARRIER_TYPE_KAFKA_HEADERS:
		return CarrierKafkaHeaders, nil
	case observabilityv1.CarrierType_CARRIER_TYPE_NONE:
		return CarrierNone, nil
	default:
		return "", fmt.Errorf("unsupported carrier %v", carrier)
	}
}

func statusSettingName(setting observabilityv1.StatusSetting) (string, error) {
	switch setting {
	case observabilityv1.StatusSetting_STATUS_SETTING_UNSET:
		return StatusUnset, nil
	case observabilityv1.StatusSetting_STATUS_SETTING_ERROR:
		return StatusError, nil
	case observabilityv1.StatusSetting_STATUS_SETTING_OK:
		return StatusOK, nil
	default:
		return "", fmt.Errorf("unspecified status setting")
	}
}

func runtimeStatusName(status observabilityv1.RuntimeStatus) (string, error) {
	switch status {
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK:
		return RuntimeStatusOK, nil
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR:
		return RuntimeStatusError, nil
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED:
		return RuntimeStatusCancelled, nil
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT:
		return RuntimeStatusTimeout, nil
	case observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN:
		return RuntimeStatusUnknown, nil
	default:
		return "", fmt.Errorf("unspecified runtime status")
	}
}

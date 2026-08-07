package generator

import (
	"strings"
	"unicode"
)

// ValidateMetrics runs semantic validation over a metrics.yaml document.
// The result is deterministic; a nil or empty slice means valid.
func ValidateMetrics(document *MetricsDocument) []*ValidationError {
	if document == nil {
		return []*ValidationError{{Document: "metrics", Field: "$", Message: "document is nil"}}
	}
	emit := newEmitter("metrics")
	validateHeader(DocumentTypeMetrics, SchemaVersionMetrics, document.DocumentType, document.SchemaVersion, document.Source, document.GeneratedBy, emit)

	metricIDs := make(map[string]bool, len(document.Metrics))
	metricNames := make(map[string]bool, len(document.Metrics))
	for index, metric := range document.Metrics {
		field := metricField(index)
		if metric.ID == "" {
			emit.emit(field+".id", "metric ID is empty")
		} else if metricIDs[metric.ID] {
			emit.emit(field+".id", "duplicate metric ID")
		} else {
			metricIDs[metric.ID] = true
		}
		if metric.Name == "" {
			emit.emit(field+".name", "metric name is empty")
		} else if metricNames[metric.Name] {
			emit.emit(field+".name", "duplicate metric name")
		} else {
			metricNames[metric.Name] = true
		}
		if !oneOf(metric.Type, MetricTypeCounter, MetricTypeHistogram, MetricTypeGauge, MetricTypeSummary) {
			emit.emit(field+".type", "unknown metric type "+quote(metric.Type))
		}
		if !validUnit(metric.Unit) {
			emit.emit(field+".unit", "unit must not contain whitespace or control characters")
		}
		validateTarget(field+".target", metric.Target, emit)
		if !oneOf(metric.Record.Trigger, TriggerStart, TriggerEnd, TriggerStateChange) {
			emit.emit(field+".record.trigger", "unknown trigger "+quote(metric.Record.Trigger))
		}
		validateValueBinding(field+".record.value", metric.Record.Value, emit)

		switch metric.Type {
		case MetricTypeHistogram:
			emitBuckets(field, metric.Buckets, emit)
			if len(metric.Quantiles) > 0 {
				emit.emit(field+".quantiles", "histogram must not declare quantiles")
			}
		case MetricTypeSummary:
			emitQuantiles(field, metric.Quantiles, emit)
			if len(metric.Buckets) > 0 {
				emit.emit(field+".buckets", "summary must not declare buckets")
			}
		case MetricTypeCounter, MetricTypeGauge:
			if len(metric.Buckets) > 0 {
				emit.emit(field+".buckets", metric.Type+" must not declare buckets")
			}
			if len(metric.Quantiles) > 0 {
				emit.emit(field+".quantiles", metric.Type+" must not declare quantiles")
			}
		}

		attributeKeys := make(map[string]bool, len(metric.Attributes))
		for attributeIndex, attribute := range metric.Attributes {
			attributeField := field + ".attributes[" + itoa(attributeIndex) + "]"
			if attribute.Key == "" {
				emit.emit(attributeField+".key", "attribute key is empty")
			} else if attributeKeys[attribute.Key] {
				emit.emit(attributeField+".key", "duplicate attribute key "+quote(attribute.Key))
			} else {
				attributeKeys[attribute.Key] = true
			}
			if attribute.Type != "" && !validValueType(attribute.Type) {
				emit.emit(attributeField+".type", "unknown value type "+quote(attribute.Type))
			}
			validateValueBinding(attributeField+".binding", attribute.Binding, emit)
		}
	}
	return emit.result()
}

// ValidateOTel runs semantic validation over an otel.yaml document.
func ValidateOTel(document *OTelDocument) []*ValidationError {
	if document == nil {
		return []*ValidationError{{Document: "otel", Field: "$", Message: "document is nil"}}
	}
	emit := newEmitter("otel")
	validateHeader(DocumentTypeOTel, SchemaVersionOTel, document.DocumentType, document.SchemaVersion, document.Source, document.GeneratedBy, emit)
	if document.PlanKind != OTelPlanKind {
		emit.emit("plan_kind", "plan_kind must be "+quote(OTelPlanKind)+" (this file is an instrumentation plan, not a Collector configuration)")
	}
	if document.SemanticConventionsVersion != OTelSemanticConventionsVersion {
		emit.emit("semantic_conventions_version", "must be pinned to "+quote(OTelSemanticConventionsVersion))
	}

	for index, resource := range document.Resources {
		field := "resources[" + itoa(index) + "]"
		if resource.Key == "" {
			emit.emit(field+".key", "resource key is empty")
		}
		if resource.Type != "" && !validValueType(resource.Type) {
			emit.emit(field+".type", "unknown value type "+quote(resource.Type))
		}
		validateValueBinding(field+".binding", resource.Binding, emit)
	}

	spanIDs := make(map[string]bool, len(document.Spans))
	spanNames := make(map[string]bool, len(document.Spans))
	eventIDs := make(map[string]bool, len(document.Spans))
	for index, span := range document.Spans {
		field := spanField(index)
		if span.ID == "" {
			emit.emit(field+".id", "span ID is empty")
		} else if spanIDs[span.ID] {
			emit.emit(field+".id", "duplicate span ID")
		} else {
			spanIDs[span.ID] = true
		}
		if span.Name == "" {
			emit.emit(field+".name", "span name is empty")
		} else if spanNames[span.Name] {
			emit.emit(field+".name", "duplicate span name")
		} else {
			spanNames[span.Name] = true
		}
		if !oneOf(span.Kind, SpanKindServer, SpanKindClient, SpanKindProducer, SpanKindConsumer, SpanKindInternal) {
			emit.emit(field+".kind", "unknown span kind "+quote(span.Kind))
		}
		validateTarget(field+".target", span.Target, emit)
		if !oneOf(span.Lifecycle.Start, TriggerStart, TriggerEnd, TriggerStateChange) {
			emit.emit(field+".lifecycle.start", "unknown trigger "+quote(span.Lifecycle.Start))
		}
		if !oneOf(span.Lifecycle.End, TriggerStart, TriggerEnd, TriggerStateChange) {
			emit.emit(field+".lifecycle.end", "unknown trigger "+quote(span.Lifecycle.End))
		}
		validateParent(field+".parent", span.Parent, spanIDs, emit)

		for attributeIndex, attribute := range span.Attributes {
			attributeField := field + ".attributes[" + itoa(attributeIndex) + "]"
			if attribute.Key == "" {
				emit.emit(attributeField+".key", "attribute key is empty")
			}
			if attribute.Type != "" && !validValueType(attribute.Type) {
				emit.emit(attributeField+".type", "unknown value type "+quote(attribute.Type))
			}
			validateValueBinding(attributeField+".binding", attribute.Binding, emit)
		}

		if span.Status != nil {
			validateStatusPolicy(field+".status", span.Status, emit)
		}

		for eventIndex, event := range span.Events {
			eventField := field + ".events[" + itoa(eventIndex) + "]"
			if event.ID == "" {
				emit.emit(eventField+".id", "event ID is empty")
			} else if eventIDs[event.ID] {
				emit.emit(eventField+".id", "duplicate event ID")
			} else {
				eventIDs[event.ID] = true
			}
			if event.Name == "" {
				emit.emit(eventField+".name", "event name is empty")
			}
			if event.Condition != "" && !oneOf(event.Condition,
				EventConditionStatusIsError, EventConditionStatusIsTimeout, EventConditionStatusIsCancelled) {
				emit.emit(eventField+".condition", "unknown event condition "+quote(event.Condition))
			}
			for statusIndex, status := range event.Statuses {
				if !oneOf(status, RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown) {
					emit.emit(eventField+".statuses["+itoa(statusIndex)+"]", "unknown runtime status "+quote(status))
				}
			}
			for attributeIndex, attribute := range event.Attributes {
				attributeField := eventField + ".attributes[" + itoa(attributeIndex) + "]"
				if attribute.Key == "" {
					emit.emit(attributeField+".key", "attribute key is empty")
				}
				validateValueBinding(attributeField+".binding", attribute.Binding, emit)
			}
		}
	}
	return emit.result()
}

// ValidateLogging runs semantic validation over a logging.yaml document.
func ValidateLogging(document *LoggingDocument) []*ValidationError {
	if document == nil {
		return []*ValidationError{{Document: "logging", Field: "$", Message: "document is nil"}}
	}
	emit := newEmitter("logging")
	validateHeader(DocumentTypeLogging, SchemaVersionLogging, document.DocumentType, document.SchemaVersion, document.Source, document.GeneratedBy, emit)

	if !document.Redaction.Immutable {
		emit.emit("redaction.immutable", "built-in redaction rules are immutable and must be true")
	}
	if len(document.Redaction.FieldNames) == 0 {
		emit.emit("redaction.field_names", "at least one redacted field name is required")
	}
	for index, name := range document.Redaction.FieldNames {
		if name == "" {
			emit.emit("redaction.field_names["+itoa(index)+"]", "redacted field name is empty")
		}
	}

	eventIDs := make(map[string]bool, len(document.Events))
	// Event names are unique per target: every dependency may emit the
	// same "dependency.operation.failed" family, so the same name across
	// different targets is meaningful, never a duplicate.
	eventNames := make(map[string]map[string]bool)
	for index, event := range document.Events {
		field := logEventField(index)
		if event.ID == "" {
			emit.emit(field+".id", "event ID is empty")
		} else if eventIDs[event.ID] {
			emit.emit(field+".id", "duplicate event ID")
		} else {
			eventIDs[event.ID] = true
		}
		if event.EventName == "" {
			emit.emit(field+".event_name", "event name is empty")
		} else {
			targetKey := event.Target.Type + ":" + event.Target.ID
			if eventNames[targetKey] == nil {
				eventNames[targetKey] = make(map[string]bool)
			}
			if eventNames[targetKey][event.EventName] {
				emit.emit(field+".event_name", "duplicate event name for the same target")
			} else {
				eventNames[targetKey][event.EventName] = true
			}
		}
		validateTarget(field+".target", event.Target, emit)
		if !oneOf(event.Trigger, TriggerStart, TriggerEnd, TriggerStateChange) {
			emit.emit(field+".trigger", "unknown trigger "+quote(event.Trigger))
		}
		if !oneOf(event.Severity.Constant, LogSeverityInfo, LogSeverityWarn, LogSeverityError) {
			emit.emit(field+".severity.constant", "unknown severity "+quote(event.Severity.Constant))
		}
		if len(event.Condition.StatusIn) == 0 {
			emit.emit(field+".condition.status_in", "at least one status is required")
		}
		seenStatus := make(map[string]bool, len(event.Condition.StatusIn))
		for statusIndex, status := range event.Condition.StatusIn {
			statusField := field + ".condition.status_in[" + itoa(statusIndex) + "]"
			if !oneOf(status, RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown) {
				emit.emit(statusField, "unknown runtime status "+quote(status))
			} else if seenStatus[status] {
				emit.emit(statusField, "duplicate status "+quote(status))
			} else {
				seenStatus[status] = true
			}
		}

		fieldKeys := make(map[string]bool, len(event.Fields))
		for fieldIndex, logField := range event.Fields {
			fieldField := field + ".fields[" + itoa(fieldIndex) + "]"
			normalized := normalizeKey(logField.Key)
			if logField.Key == "" {
				emit.emit(fieldField+".key", "field key is empty")
			} else if fieldKeys[normalized] {
				emit.emit(fieldField+".key", "duplicate field key "+quote(logField.Key))
			} else {
				fieldKeys[normalized] = true
			}
			if !validValueType(logField.Type) {
				emit.emit(fieldField+".type", "unknown value type "+quote(logField.Type))
			}
			validateValueBinding(fieldField+".binding", logField.Binding, emit)
			switch normalized {
			case "traceid", "spanid":
				// Correlation IDs must come from the runtime context; they
				// may be optional when no Root Span context is provable
				// (P1-12 dependency events). The Runtime omits them when
				// absent; empty string placeholders are never emitted.
				if logField.Binding.Source != ValueSourceRuntimeContext {
					emit.emit(fieldField+".binding.source", logField.Key+" must bind to runtime_context")
				}
			case "requestid":
				if logField.Binding.Source != ValueSourceRuntimeContext {
					emit.emit(fieldField+".binding.source", "request_id must bind to runtime_context")
				}
			}
		}
	}
	return emit.result()
}

func (document *MetricsDocument) Validate() []*ValidationError { return ValidateMetrics(document) }
func (document *OTelDocument) Validate() []*ValidationError    { return ValidateOTel(document) }
func (document *LoggingDocument) Validate() []*ValidationError { return ValidateLogging(document) }

// emitter collects ValidationError values in deterministic order.
type emitter struct {
	document   string
	violations []*ValidationError
}

func newEmitter(document string) *emitter {
	return &emitter{document: document}
}

func (collector *emitter) emit(field, message string) {
	collector.violations = append(collector.violations, &ValidationError{
		Document: collector.document,
		Field:    field,
		Message:  message,
	})
}

func (collector *emitter) result() []*ValidationError {
	return collector.violations
}

func validateHeader(expectedDocumentType, expectedSchemaVersion, documentType, schemaVersion string, source Source, by GeneratedBy, emit *emitter) {
	switch {
	case schemaVersion == "":
		emit.emit("schema_version", "schema version is empty")
	case schemaVersion != expectedSchemaVersion:
		emit.emit("schema_version", "unsupported schema version "+quote(schemaVersion)+", expected "+quote(expectedSchemaVersion))
	}
	switch {
	case documentType == "":
		emit.emit("document_type", "document type is empty")
	case documentType != expectedDocumentType:
		emit.emit("document_type", "document type "+quote(documentType)+" does not match expected "+quote(expectedDocumentType))
	}
	if source.IRSchemaVersion == "" {
		emit.emit("source.ir_schema_version", "IR schema version is empty")
	}
	if source.ServiceName == "" {
		emit.emit("source.service_name", "service name is empty")
	}
	if by.Name == "" {
		emit.emit("generated_by.name", "generator name is empty")
	}
	if by.Version == "" {
		emit.emit("generated_by.version", "generator version is empty")
	}
}

func validateTarget(field string, target TargetRef, emit *emitter) {
	if !oneOf(target.Type, TargetKindEndpoint, TargetKindFunction, TargetKindDependency, TargetKindCallEdge) {
		emit.emit(field+".type", "unknown target type "+quote(target.Type))
	}
	if target.ID == "" {
		emit.emit(field+".id", "target ID is empty")
	}
}

func validateValueBinding(field string, binding ValueBinding, emit *emitter) {
	if binding.Source == "" {
		emit.emit(field+".source", "binding source is empty")
		return
	}
	if !oneOf(binding.Source, ValueSourceConstant, ValueSourceIR,
		ValueSourceRuntimeResult, ValueSourceRuntimeResource, ValueSourceRuntimeContext, ValueSourceRuntimeClock) {
		emit.emit(field+".source", "unknown binding source "+quote(binding.Source))
	}
	if binding.Source == ValueSourceConstant {
		constants := 0
		if binding.String != "" {
			constants++
		}
		if binding.Number != 0 {
			constants++
		}
		if binding.Bool != nil {
			constants++
		}
		if constants == 0 {
			emit.emit(field, "constant binding must set exactly one of string, number or bool")
		} else if constants > 1 {
			emit.emit(field, "constant binding must set exactly one of string, number or bool")
		}
	} else if binding.Path == "" {
		emit.emit(field+".path", "path is required for source "+quote(binding.Source))
	}
	if binding.Type != "" && !validValueType(binding.Type) {
		emit.emit(field+".type", "unknown value type "+quote(binding.Type))
	}
	for index, allowed := range binding.AllowedValues {
		if !oneOf(allowed, RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown) {
			emit.emit(field+".allowed_values["+itoa(index)+"]", "unknown runtime status "+quote(allowed))
		}
	}
}

func validateParent(field string, parent Parent, spanIDs map[string]bool, emit *emitter) {
	switch parent.Strategy {
	case ParentExtractOrRoot:
		if !oneOf(parent.Carrier, CarrierHTTPHeaders, CarrierGRPCMetadata, CarrierKafkaHeaders, CarrierNone) {
			emit.emit(field+".carrier", "extract_or_root requires a valid carrier")
		}
		if parent.StaticParentSpanID != "" {
			emit.emit(field+".static_parent_span_id", "extract_or_root must not set a static parent")
		}
	case ParentNewRoot:
		if parent.Carrier != "" {
			emit.emit(field+".carrier", "new_root must not set a carrier")
		}
		if parent.StaticParentSpanID != "" {
			emit.emit(field+".static_parent_span_id", "new_root must not set a static parent")
		}
	case ParentCurrentContext:
		if parent.Carrier != "" {
			emit.emit(field+".carrier", "current_context must not set a carrier")
		}
		if parent.StaticParentSpanID != "" {
			emit.emit(field+".static_parent_span_id", "current_context must not set a static parent")
		}
	case ParentStatic:
		if parent.StaticParentSpanID == "" {
			emit.emit(field+".static_parent_span_id", "static parent requires a static_parent_span_id")
		} else if !spanIDs[parent.StaticParentSpanID] {
			emit.emit(field+".static_parent_span_id", "static parent references unknown span ID "+quote(parent.StaticParentSpanID))
		}
		if parent.Carrier != "" {
			emit.emit(field+".carrier", "static must not set a carrier")
		}
	default:
		emit.emit(field+".strategy", "unknown parent strategy "+quote(parent.Strategy))
	}
}

func validateStatusPolicy(field string, status *StatusPolicy, emit *emitter) {
	if status.Mapping == nil {
		emit.emit(field+".mapping", "status mapping is missing")
		return
	}
	required := []string{RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown}
	covered := make(map[string]bool, len(status.Mapping))
	for runtimeStatus, setting := range status.Mapping {
		if !oneOf(runtimeStatus, required...) {
			emit.emit(field+".mapping", "unknown runtime status "+quote(runtimeStatus))
			continue
		}
		covered[runtimeStatus] = true
		if !oneOf(setting, StatusUnset, StatusError, StatusOK) {
			emit.emit(field+".mapping."+runtimeStatus, "unknown status setting "+quote(setting))
		}
	}
	for _, runtimeStatus := range required {
		if !covered[runtimeStatus] {
			emit.emit(field+".mapping", "status mapping must cover "+quote(runtimeStatus))
		}
	}
}

func emitBuckets(field string, buckets []float64, emit *emitter) {
	if len(buckets) == 0 {
		emit.emit(field+".buckets", "histogram requires at least one bucket boundary")
		return
	}
	for index, boundary := range buckets {
		if boundary <= 0 {
			emit.emit(field+".buckets["+itoa(index)+"]", "bucket boundary must be positive")
		}
		if index > 0 && buckets[index-1] >= boundary {
			emit.emit(field+".buckets", "bucket boundaries must be strictly increasing and unique")
			return
		}
	}
}

func emitQuantiles(field string, quantiles []float64, emit *emitter) {
	if len(quantiles) == 0 {
		emit.emit(field+".quantiles", "summary requires at least one quantile")
		return
	}
	if len(quantiles) > 10 {
		emit.emit(field+".quantiles", "summary supports at most 10 quantiles")
	}
	for index, quantile := range quantiles {
		if quantile <= 0 || quantile >= 1 {
			emit.emit(field+".quantiles["+itoa(index)+"]", "quantile must be within (0,1)")
		}
		if index > 0 && quantiles[index-1] >= quantile {
			emit.emit(field+".quantiles", "quantiles must be strictly increasing and unique")
			return
		}
	}
}

func validUnit(unit string) bool {
	for _, r := range unit {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validValueType(valueType string) bool {
	return oneOf(valueType, ValueTypeString, ValueTypeInt, ValueTypeDouble, ValueTypeBool, ValueTypeStatus, ValueTypeTimestamp, ValueTypeNumber)
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

// normalizeKey lower-cases a field key and strips '-' and '_' so semantic
// duplicates are detected across case and separator variants.
func normalizeKey(key string) string {
	var builder strings.Builder
	for _, r := range key {
		switch {
		case r == '-' || r == '_':
			continue
		default:
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return builder.String()
}

func quote(value string) string {
	return strconvQuote(value)
}

func metricField(index int) string   { return "metrics[" + itoa(index) + "]" }
func spanField(index int) string     { return "spans[" + itoa(index) + "]" }
func logEventField(index int) string { return "events[" + itoa(index) + "]" }

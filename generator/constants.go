package generator

// Published v1 schema versions. Each is a closed contract: the field set is
// fixed once published; any field or enum change requires a new explicit
// version.
const (
	// SchemaVersionMetrics is the metrics.yaml contract version.
	SchemaVersionMetrics = "generator.metrics/v1"
	// SchemaVersionOTel is the otel.yaml contract version.
	SchemaVersionOTel = "generator.otel/v1"
	// SchemaVersionLogging is the logging.yaml contract version.
	SchemaVersionLogging = "generator.logging/v1"

	// DocumentTypeMetrics is the fixed document_type of metrics.yaml.
	DocumentTypeMetrics = "instrumentation.metrics"
	// DocumentTypeOTel is the fixed document_type of otel.yaml.
	DocumentTypeOTel = "instrumentation.tracing"
	// DocumentTypeLogging is the fixed document_type of logging.yaml.
	DocumentTypeLogging = "instrumentation.logging"

	// OTelPlanKind marks otel.yaml as an instrumentation plan, never a
	// Collector configuration.
	OTelPlanKind = "instrumentation"

	// OTelSemanticConventionsVersion is the fixed OpenTelemetry Semantic
	// Conventions version pinned for Phase 1. It is not user-configurable.
	OTelSemanticConventionsVersion = "1.37.0"
)

// Enum vocabularies. The YAML contract uses stable lowercase names.
const (
	// MetricType values.
	MetricTypeCounter   = "counter"
	MetricTypeHistogram = "histogram"
	MetricTypeGauge     = "gauge"
	MetricTypeSummary   = "summary"

	// SpanKind values mirror OpenTelemetry kinds by name only.
	SpanKindServer   = "server"
	SpanKindClient   = "client"
	SpanKindProducer = "producer"
	SpanKindConsumer = "consumer"
	SpanKindInternal = "internal"

	// TargetKind values reference IR entities.
	TargetKindEndpoint   = "endpoint"
	TargetKindFunction   = "function"
	TargetKindDependency = "dependency"
	TargetKindCallEdge   = "call_edge"

	// TriggerPhase values.
	TriggerStart       = "operation_start"
	TriggerEnd         = "operation_end"
	TriggerStateChange = "state_change"

	// ValueSource values.
	ValueSourceConstant        = "constant"
	ValueSourceIR              = "ir"
	ValueSourceRuntimeResult   = "runtime_result"
	ValueSourceRuntimeResource = "runtime_resource"
	ValueSourceRuntimeContext  = "runtime_context"
	ValueSourceRuntimeClock    = "runtime_clock"

	// ValueType values.
	ValueTypeString    = "string"
	ValueTypeInt       = "int"
	ValueTypeDouble    = "double"
	ValueTypeBool      = "bool"
	ValueTypeStatus    = "status"
	ValueTypeTimestamp = "timestamp"
	// ValueTypeNumber is accepted as an alias of double; the file contract
	// examples use "number" for numeric fields.
	ValueTypeNumber = "number"

	// ParentStrategyMode values.
	ParentExtractOrRoot  = "extract_or_root"
	ParentNewRoot        = "new_root"
	ParentCurrentContext = "current_context"
	ParentStatic         = "static"

	// Carrier values.
	CarrierHTTPHeaders  = "http_headers"
	CarrierGRPCMetadata = "grpc_metadata"
	CarrierKafkaHeaders = "kafka_headers"
	CarrierNone         = "none"

	// StatusSetting values (OTel-compatible span status output).
	StatusUnset = "unset"
	StatusError = "error"
	StatusOK    = "ok"

	// RuntimeStatus vocabulary (finite, exactly these five).
	RuntimeStatusOK        = "ok"
	RuntimeStatusError     = "error"
	RuntimeStatusCancelled = "cancelled"
	RuntimeStatusTimeout   = "timeout"
	RuntimeStatusUnknown   = "unknown"

	// LogSeverity values.
	LogSeverityInfo  = "info"
	LogSeverityWarn  = "warn"
	LogSeverityError = "error"

	// SpanEventCondition values.
	EventConditionStatusIsError     = "status_is_error"
	EventConditionStatusIsTimeout   = "status_is_timeout"
	EventConditionStatusIsCancelled = "status_is_cancelled"
)

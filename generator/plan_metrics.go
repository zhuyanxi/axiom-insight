package generator

import (
	"fmt"
	"strings"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// Renderer constants for the generated_by header.
const (
	// GeneratorName identifies the generating tool.
	GeneratorName = "si"
	// GeneratorVersion is the renderer's contract version; the CLI may
	// substitute its own release version at the writer stage.
	GeneratorVersion = "v0.2.0"
)

// RenderMetricsPlan converts the metrics section of a validated
// GenerationPlan into the closed metrics.yaml contract and renders it to
// canonical bytes. The conversion copies fields from the plan verbatim —
// it never re-derives names, attributes or security decisions — and the
// rendered bytes are immediately re-parsed with the strict decoder and
// re-validated semantically before being returned.
//
// Errors carry the GEN_RENDER_ERROR context and no partial bytes are ever
// returned. The function touches no filesystem, environment, clock or
// network: identical inputs produce identical bytes everywhere.
func RenderMetricsPlan(plan *observabilityv1.GenerationPlan, policy policy.Policy) ([]byte, error) {
	document, err := metricsDocument(plan, policy)
	if err != nil {
		return nil, err
	}
	contents, err := RenderMetrics(document)
	if err != nil {
		return nil, renderFailure("", err)
	}
	if _, err := DecodeMetrics(contents); err != nil {
		return nil, renderFailure("", err)
	}
	return contents, nil
}

// metricsDocument converts the plan into the typed metrics.yaml model.
func metricsDocument(plan *observabilityv1.GenerationPlan, policy policy.Policy) (*MetricsDocument, error) {
	if plan == nil {
		return nil, renderFailure("$", fmt.Errorf("plan is nil"))
	}
	if plan.GetSchemaVersion() != observabilityv1.GenerationPlanSchemaVersion {
		return nil, renderFailure("$", fmt.Errorf(
			"plan schema version %q is not supported; supported: %s",
			plan.GetSchemaVersion(), observabilityv1.GenerationPlanSchemaVersion))
	}
	document := &MetricsDocument{
		SchemaVersion: SchemaVersionMetrics,
		DocumentType:  DocumentTypeMetrics,
		Source: Source{
			IRSchemaVersion: plan.GetSourceIrSchemaVersion(),
			ServiceName:     plan.GetServiceName(),
		},
		GeneratedBy: GeneratedBy{Name: GeneratorName, Version: GeneratorVersion},
	}
	document.Metrics = make([]Metric, 0, len(plan.GetMetrics()))
	for _, metric := range plan.GetMetrics() {
		converted, err := convertMetric(metric, policy)
		if err != nil {
			return nil, renderFailure(metric.GetId(), err)
		}
		document.Metrics = append(document.Metrics, converted)
	}
	return document, nil
}

func convertMetric(metric *observabilityv1.MetricPlan, policy policy.Policy) (Metric, error) {
	metricType, err := metricTypeName(metric.GetType())
	if err != nil {
		return Metric{}, err
	}
	trigger, err := triggerPhaseName(metric.GetTrigger().GetPhase())
	if err != nil {
		return Metric{}, err
	}
	value, err := convertValueBinding(metric.GetValue())
	if err != nil {
		return Metric{}, err
	}
	target, err := convertTarget(metric.GetTarget())
	if err != nil {
		return Metric{}, err
	}
	converted := Metric{
		ID:          metric.GetId(),
		Name:        metric.GetName(),
		Type:        metricType,
		Unit:        metric.GetUnit(),
		Description: metric.GetDescription(),
		Target:      target,
		Record:      Record{Trigger: trigger, Value: value},
	}
	if metric.GetId() == "" {
		return Metric{}, fmt.Errorf("metric id is empty")
	}
	if metric.GetName() == "" {
		return Metric{}, fmt.Errorf("metric name is empty")
	}
	switch metric.GetType() {
	case observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM:
		converted.Buckets = append([]float64(nil), policy.Metrics.HistogramBucketsSeconds...)
	case observabilityv1.MetricType_METRIC_TYPE_SUMMARY:
		converted.Quantiles = append([]float64(nil), policy.Metrics.Summaries.Quantiles...)
	}
	for _, attribute := range metric.GetAttributes() {
		key := attribute.GetKey()
		if key == "" {
			return Metric{}, fmt.Errorf("attribute key is empty")
		}
		attributeValue, err := convertValueBinding(attribute.GetValue())
		if err != nil {
			return Metric{}, fmt.Errorf("attribute %q: %w", key, err)
		}
		converted.Attributes = append(converted.Attributes, Attribute{
			Key:     key,
			Type:    bindingTypeName(attribute.GetValue().GetType()),
			Binding: attributeValue,
		})
	}
	return converted, nil
}

func convertTarget(target *observabilityv1.TargetRef) (TargetRef, error) {
	if target == nil {
		return TargetRef{}, fmt.Errorf("target reference is missing")
	}
	kind, err := targetKindName(target.GetKind())
	if err != nil {
		return TargetRef{}, err
	}
	if target.GetId() == "" {
		return TargetRef{}, fmt.Errorf("target id is empty")
	}
	return TargetRef{Type: kind, ID: target.GetId()}, nil
}

// convertValueBinding maps a plan binding onto the YAML contract. Plan
// constants follow the convention that the value lives in the constant
// path suffix: "plan.constant.one" is the numeric literal 1 and
// "plan.constant.<text>" is the string <text>. Runtime and IR bindings
// copy their path verbatim; status bindings carry the finite vocabulary.
func convertValueBinding(binding *observabilityv1.ValueBinding) (ValueBinding, error) {
	if binding == nil {
		return ValueBinding{}, fmt.Errorf("value binding is missing")
	}
	source, err := valueSourceName(binding.GetSource())
	if err != nil {
		return ValueBinding{}, err
	}
	converted := ValueBinding{Source: source, Type: bindingTypeName(binding.GetType())}
	switch binding.GetSource() {
	case observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT:
		path := binding.GetPath()
		if !strings.HasPrefix(path, "plan.constant.") {
			return ValueBinding{}, fmt.Errorf("plan constant path %q must start with %q", path, "plan.constant.")
		}
		value := strings.TrimPrefix(path, "plan.constant.")
		if value == "one" {
			// The canonical counter value: the numeric literal 1.
			converted.Number = 1
		} else {
			converted.String = value
		}
	case observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
		observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
		observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT,
		observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CLOCK:
		converted.Path = binding.GetPath()
		if binding.GetPath() == "" {
			return ValueBinding{}, fmt.Errorf("binding path is empty for source %s", source)
		}
	default:
		return ValueBinding{}, fmt.Errorf("unsupported value source %s", source)
	}
	if binding.GetRequired() {
		converted.Required = new(true)
	}
	if binding.GetFallback() != "" {
		converted.Fallback = binding.GetFallback()
	}
	if binding.GetType() == observabilityv1.ValueType_VALUE_TYPE_STATUS {
		converted.AllowedValues = []string{
			RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled,
			RuntimeStatusTimeout, RuntimeStatusUnknown,
		}
	}
	return converted, nil
}

// CheckPlanPolicyConsistency reports plan/policy mismatches the renderer
// will not paper over: a policy that enables summaries while the plan
// contains none, or a plan carrying summaries while the policy disables
// them. The renderer renders the plan as-is; the caller decides whether
// the issues are fatal.
func CheckPlanPolicyConsistency(plan *observabilityv1.GenerationPlan, policy policy.Policy) []ConsistencyIssue {
	if plan == nil {
		return nil
	}
	var issues []ConsistencyIssue
	hasSummary := false
	for _, metric := range plan.GetMetrics() {
		if metric.GetType() == observabilityv1.MetricType_METRIC_TYPE_SUMMARY {
			hasSummary = true
		}
	}
	if policy.Metrics.Summaries.Enabled && !hasSummary {
		issues = append(issues, ConsistencyIssue{
			Message: "policy enables summaries but the plan contains no summary instruments",
		})
	}
	if !policy.Metrics.Summaries.Enabled && hasSummary {
		issues = append(issues, ConsistencyIssue{
			Message: "plan contains summary instruments while the policy disables summaries",
		})
	}
	return issues
}

// ConsistencyIssue describes one plan/policy mismatch.
type ConsistencyIssue struct {
	// Message explains the mismatch; it never contains plan values.
	Message string
}

func renderFailure(itemID string, err error) error {
	context := "$"
	if itemID != "" {
		context = "metric " + itemID
	}
	return fmt.Errorf("GEN_RENDER_ERROR: %s: %w", context, err)
}

func metricTypeName(metricType observabilityv1.MetricType) (string, error) {
	switch metricType {
	case observabilityv1.MetricType_METRIC_TYPE_COUNTER:
		return MetricTypeCounter, nil
	case observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM:
		return MetricTypeHistogram, nil
	case observabilityv1.MetricType_METRIC_TYPE_GAUGE:
		return MetricTypeGauge, nil
	case observabilityv1.MetricType_METRIC_TYPE_SUMMARY:
		return MetricTypeSummary, nil
	default:
		return "", fmt.Errorf("unsupported metric type %v", metricType)
	}
}

func triggerPhaseName(phase observabilityv1.TriggerPhase) (string, error) {
	switch phase {
	case observabilityv1.TriggerPhase_TRIGGER_PHASE_START:
		return TriggerStart, nil
	case observabilityv1.TriggerPhase_TRIGGER_PHASE_END:
		return TriggerEnd, nil
	case observabilityv1.TriggerPhase_TRIGGER_PHASE_STATE_CHANGE:
		return TriggerStateChange, nil
	default:
		return "", fmt.Errorf("unsupported trigger phase %v", phase)
	}
}

func targetKindName(kind observabilityv1.TargetKind) (string, error) {
	switch kind {
	case observabilityv1.TargetKind_TARGET_KIND_ENDPOINT:
		return TargetKindEndpoint, nil
	case observabilityv1.TargetKind_TARGET_KIND_FUNCTION:
		return TargetKindFunction, nil
	case observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY:
		return TargetKindDependency, nil
	case observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE:
		return TargetKindCallEdge, nil
	default:
		return "", fmt.Errorf("unsupported target kind %v", kind)
	}
}

func valueSourceName(source observabilityv1.ValueSource) (string, error) {
	switch source {
	case observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT:
		return ValueSourceConstant, nil
	case observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT:
		return ValueSourceIR, nil
	case observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT:
		return ValueSourceRuntimeResult, nil
	case observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE:
		return ValueSourceRuntimeResource, nil
	case observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT:
		return ValueSourceRuntimeContext, nil
	case observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CLOCK:
		return ValueSourceRuntimeClock, nil
	default:
		return "", fmt.Errorf("unsupported value source %v", source)
	}
}

func bindingTypeName(valueType observabilityv1.ValueType) string {
	switch valueType {
	case observabilityv1.ValueType_VALUE_TYPE_STRING:
		return ValueTypeString
	case observabilityv1.ValueType_VALUE_TYPE_INT64:
		return ValueTypeInt
	case observabilityv1.ValueType_VALUE_TYPE_DOUBLE:
		return ValueTypeDouble
	case observabilityv1.ValueType_VALUE_TYPE_BOOL:
		return ValueTypeBool
	case observabilityv1.ValueType_VALUE_TYPE_STATUS:
		return ValueTypeStatus
	case observabilityv1.ValueType_VALUE_TYPE_TIMESTAMP:
		return ValueTypeTimestamp
	default:
		return ""
	}
}

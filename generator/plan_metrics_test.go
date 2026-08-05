package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// renderMetricsPlanFixture builds a plan covering all four metric types
// plus a runtime fallback binding, in a fixed (unsorted) order.
func renderMetricsPlanFixture() *observabilityv1.GenerationPlan {
	base := func(id, name string, metricType observabilityv1.MetricType, unit, description string, targetKind observabilityv1.TargetKind, targetID string, trigger observabilityv1.TriggerPhase, value *observabilityv1.ValueBinding) *observabilityv1.MetricPlan {
		return &observabilityv1.MetricPlan{
			Id: id, Name: name, Type: metricType, Unit: unit, Description: description,
			Target:  &observabilityv1.TargetRef{Kind: targetKind, Id: targetID},
			Trigger: &observabilityv1.Trigger{Phase: trigger},
			Value:   value,
			Attributes: []*observabilityv1.AttributeBinding{
				{Key: "status", Value: statusBinding()},
				{Key: "service", Value: irBinding("service.name")},
				{Key: "operation", Value: constantBinding("plan.constant.post")},
			},
		}
	}
	return &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "orders",
		Metrics: []*observabilityv1.MetricPlan{
			base("metrics:dep:sql:count", "orders_orders_store_exec_sql_operations_total", observabilityv1.MetricType_METRIC_TYPE_COUNTER,
				"{operation}", "Number of SQL operations completed; unit {operation}; recorded at operation end",
				observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, "dep:sql", observabilityv1.TriggerPhase_TRIGGER_PHASE_END,
				constantBinding("plan.constant.one")),
			base("metrics:ep:orders:duration", "orders_orders_handleorder_post_http_request_duration_seconds", observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM,
				"s", "Duration of HTTP requests; unit s; recorded at operation end",
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, "ep:orders", observabilityv1.TriggerPhase_TRIGGER_PHASE_END,
				runtimeBinding("runtime.operation.duration_seconds")),
			base("metrics:ep:orders:in_flight", "orders_orders_handleorder_post_http_requests_in_flight", observabilityv1.MetricType_METRIC_TYPE_GAUGE,
				"{operation}", "Number of HTTP requests in flight; unit {operation}; recorded on state change",
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, "ep:orders", observabilityv1.TriggerPhase_TRIGGER_PHASE_STATE_CHANGE,
				runtimeBinding("runtime.operation.in_flight")),
			base("metrics:dep:sql:duration_summary", "orders_orders_store_exec_sql_operation_duration_seconds_summary", observabilityv1.MetricType_METRIC_TYPE_SUMMARY,
				"s", "Duration summary of SQL operations; unit s; recorded at operation end",
				observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, "dep:sql", observabilityv1.TriggerPhase_TRIGGER_PHASE_END,
				runtimeBinding("runtime.operation.duration_seconds")),
		},
	}
}

func constantBinding(path string) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func irBinding(path string) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func runtimeBinding(path string) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_DOUBLE, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		Required: true, Fallback: "unknown",
	}
}

func statusBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.operation.status", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STATUS, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

func summaryPolicy() policy.Policy {
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			Summaries: &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.9, 0.99}},
		},
	}, nil)
	if err != nil {
		panic(err)
	}
	return *resolved
}

// TestRenderMetricsPlanAC1: the four-type plan renders to a document that
// strictly decodes, passes the semantic validator and the machine schema,
// and every plan ID appears exactly once.
func TestRenderMetricsPlanAC1(t *testing.T) {
	plan := renderMetricsPlanFixture()
	contents, err := RenderMetricsPlan(plan, summaryPolicy())
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("rendered bytes are empty")
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("rendered output does not decode strictly: %v\n%s", err, contents)
	}
	if violations := document.Validate(); len(violations) > 0 {
		t.Fatalf("rendered output fails semantic validation: %v", violations)
	}
	jsonData, err := yamlToJSON(t, contents)
	if err != nil {
		t.Fatalf("rendered output does not convert to JSON: %v", err)
	}
	if err := schemacheck.Validate(loadSchema(t, "metrics.schema.json"), jsonData); err != nil {
		t.Fatalf("rendered output fails the machine schema: %v", err)
	}
	if document.DocumentType != DocumentTypeMetrics {
		t.Errorf("document type = %q", document.DocumentType)
	}
	if document.SchemaVersion != SchemaVersionMetrics {
		t.Errorf("schema version = %q", document.SchemaVersion)
	}
	if document.Source.IRSchemaVersion != "v1" || document.Source.ServiceName != "orders" {
		t.Errorf("source header = %+v", document.Source)
	}
	if document.GeneratedBy.Name != GeneratorName || document.GeneratedBy.Version != GeneratorVersion {
		t.Errorf("generated_by = %+v", document.GeneratedBy)
	}
	// Every plan ID appears exactly once, and definitions are sorted by ID.
	seen := map[string]bool{}
	previous := ""
	for index, metric := range document.Metrics {
		if seen[metric.ID] {
			t.Errorf("plan ID %q rendered twice", metric.ID)
		}
		seen[metric.ID] = true
		if previous != "" && metric.ID < previous {
			t.Errorf("metrics not sorted by ID at index %d", index)
		}
		previous = metric.ID
	}
	for _, metric := range plan.GetMetrics() {
		if !seen[metric.GetId()] {
			t.Errorf("plan ID %q missing from the rendered document", metric.GetId())
		}
	}
	// The fixture renders four instruments with four distinct types.
	types := map[string]bool{}
	for _, metric := range document.Metrics {
		types[metric.Type] = true
	}
	for _, metricType := range []string{MetricTypeCounter, MetricTypeHistogram, MetricTypeGauge, MetricTypeSummary} {
		if !types[metricType] {
			t.Errorf("metric type %s missing from rendered output", metricType)
		}
	}
}

// TestRenderMetricsPlanAC2: rendering is byte-identical across 10 runs
// and working directories; SHA-256 is stable.
func TestRenderMetricsPlanAC2(t *testing.T) {
	plan := renderMetricsPlanFixture()
	first, err := RenderMetricsPlan(plan, summaryPolicy())
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	sum := sha256.Sum256(first)
	reference := hex.EncodeToString(sum[:])
	t.Chdir(t.TempDir())
	for range 10 {
		contents, err := RenderMetricsPlan(plan, summaryPolicy())
		if err != nil {
			t.Fatalf("RenderMetricsPlan failed: %v", err)
		}
		if string(contents) != string(first) {
			t.Fatal("rendered bytes changed across runs")
		}
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != reference {
			t.Fatal("SHA-256 changed across runs")
		}
	}
}

// TestRenderMetricsPlanNoEnvironmentLeak: output never contains the
// working directory, user or environment values.
func TestRenderMetricsPlanNoEnvironmentLeak(t *testing.T) {
	t.Setenv("SI_RENDER_CANARY", "canary-env-7f3a")
	plan := renderMetricsPlanFixture()
	contents, err := RenderMetricsPlan(plan, summaryPolicy())
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	for _, canary := range []string{"canary-env-7f3a", os.Getenv("HOME"), t.TempDir()} {
		if canary != "" && strings.Contains(string(contents), canary) {
			t.Errorf("rendered bytes leak %q", canary)
		}
	}
}

// TestRenderMetricsPlanNoSummaryAC3: with summaries enabled but absent
// from the plan, the renderer renders per plan (no summary created) and
// the consistency check reports the mismatch.
func TestRenderMetricsPlanNoSummaryAC3(t *testing.T) {
	plan := renderMetricsPlanFixture()
	plan.Metrics = plan.Metrics[:3] // drop the summary
	contents, err := RenderMetricsPlan(plan, summaryPolicy())
	if err != nil {
		t.Fatalf("Renderer must render per plan: %v", err)
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	for _, metric := range document.Metrics {
		if metric.Type == MetricTypeSummary {
			t.Fatalf("renderer created a summary the plan lacks: %s", metric.ID)
		}
	}
	issues := CheckPlanPolicyConsistency(plan, summaryPolicy())
	if len(issues) != 1 {
		t.Fatalf("consistency issues = %d, want 1: %v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message, "summaries") {
		t.Errorf("issue message = %q", issues[0].Message)
	}
}

// TestRenderMetricsPlanReverseMismatch: a plan carrying summaries under a
// policy that disables them is rendered as-is and reported.
func TestRenderMetricsPlanReverseMismatch(t *testing.T) {
	plan := renderMetricsPlanFixture()
	defaults, _ := policy.Resolve(nil, nil)
	contents, err := RenderMetricsPlan(plan, *defaults)
	if err != nil {
		t.Fatalf("Renderer must render per plan: %v", err)
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	summaryCount := 0
	for _, metric := range document.Metrics {
		if metric.Type == MetricTypeSummary {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Errorf("summary count = %d, want 1 rendered per plan", summaryCount)
	}
	issues := CheckPlanPolicyConsistency(plan, *defaults)
	if len(issues) != 1 {
		t.Fatalf("consistency issues = %d, want 1: %v", len(issues), issues)
	}
}

// TestRenderMetricsPlanInvalidInputAC4: nil plans, unknown types and
// empty IDs fail with GEN_RENDER_ERROR and no partial bytes.
func TestRenderMetricsPlanInvalidInputAC4(t *testing.T) {
	defaults, _ := policy.Resolve(nil, nil)
	if _, err := RenderMetricsPlan(nil, *defaults); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("nil plan must fail with GEN_RENDER_ERROR, got %v", err)
	}

	unsupportedSchema := renderMetricsPlanFixture()
	unsupportedSchema.SchemaVersion = "v99"
	if _, err := RenderMetricsPlan(unsupportedSchema, *defaults); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("unsupported schema must fail with GEN_RENDER_ERROR, got %v", err)
	}

	unknownType := renderMetricsPlanFixture()
	unknownType.Metrics[0].Type = observabilityv1.MetricType_METRIC_TYPE_UNSPECIFIED
	if _, err := RenderMetricsPlan(unknownType, *defaults); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") ||
		!strings.Contains(err.Error(), unknownType.Metrics[0].GetId()) {
		t.Fatalf("unsupported type must fail with the metric ID context, got %v", err)
	}

	emptyID := renderMetricsPlanFixture()
	emptyID.Metrics[1].Id = ""
	if _, err := RenderMetricsPlan(emptyID, *defaults); err == nil {
		t.Fatal("empty metric ID must fail")
	}

	badConstant := renderMetricsPlanFixture()
	badConstant.Metrics[0].Value.Path = "not-a-constant-path"
	if _, err := RenderMetricsPlan(badConstant, *defaults); err == nil {
		t.Fatal("bad constant path must fail")
	}
}

// TestRenderMetricsPlanEmptyAC: an empty metrics plan renders the empty
// collection explicitly and still round-trips.
func TestRenderMetricsPlanEmptyAC(t *testing.T) {
	plan := &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "orders",
	}
	defaults, _ := policy.Resolve(nil, nil)
	contents, err := RenderMetricsPlan(plan, *defaults)
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	if len(document.Metrics) != 0 {
		t.Errorf("metric count = %d, want 0", len(document.Metrics))
	}
}

// TestRenderMetricsPlanBucketAndQuantilePolicy: histogram buckets and
// summary quantiles come from the policy; floats round-trip exactly.
func TestRenderMetricsPlanBucketAndQuantilePolicy(t *testing.T) {
	plan := renderMetricsPlanFixture()
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			HistogramBucketsSeconds: []float64{0.05, 0.5, 5},
			Summaries:               &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.99}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	contents, err := RenderMetricsPlan(plan, *resolved)
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	for _, metric := range document.Metrics {
		switch metric.Type {
		case MetricTypeHistogram:
			if len(metric.Buckets) != 3 || metric.Buckets[0] != 0.05 || metric.Buckets[2] != 5 {
				t.Errorf("histogram %s buckets = %v, want policy buckets", metric.ID, metric.Buckets)
			}
		case MetricTypeSummary:
			if len(metric.Quantiles) != 2 || metric.Quantiles[1] != 0.99 {
				t.Errorf("summary %s quantiles = %v, want policy quantiles", metric.ID, metric.Quantiles)
			}
		}
	}
}

// TestRenderMetricsPlanConstantConvention: plan constants render as
// string/number values, runtime bindings keep their paths and status
// bindings carry the finite vocabulary.
func TestRenderMetricsPlanConstantConvention(t *testing.T) {
	plan := renderMetricsPlanFixture()
	contents, err := RenderMetricsPlan(plan, summaryPolicy())
	if err != nil {
		t.Fatalf("RenderMetricsPlan failed: %v", err)
	}
	document, err := DecodeMetrics(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	counter := findMetric(document, "metrics:dep:sql:count")
	if counter.Record.Value.Source != ValueSourceConstant || counter.Record.Value.Number != 1 {
		t.Errorf("counter value = %+v, want constant number 1", counter.Record.Value)
	}
	histogram := findMetric(document, "metrics:ep:orders:duration")
	if histogram.Record.Value.Source != ValueSourceRuntimeResult ||
		histogram.Record.Value.Path != "runtime.operation.duration_seconds" {
		t.Errorf("histogram value = %+v", histogram.Record.Value)
	}
	if histogram.Record.Value.Required == nil || !*histogram.Record.Value.Required ||
		histogram.Record.Value.Fallback != "unknown" {
		t.Errorf("required binding must carry fallback/required explicitly: %+v", histogram.Record.Value)
	}
	status := findAttribute(counter, "status")
	if status == nil || len(status.Binding.AllowedValues) != 5 {
		t.Errorf("status binding must carry the finite vocabulary: %+v", status)
	}
	operation := findAttribute(counter, "operation")
	if operation == nil || operation.Binding.Source != ValueSourceConstant ||
		operation.Binding.String != "post" {
		t.Errorf("operation binding = %+v, want constant string post", operation)
	}
}

func findMetric(document *MetricsDocument, id string) Metric {
	for _, metric := range document.Metrics {
		if metric.ID == id {
			return metric
		}
	}
	return Metric{}
}

func findAttribute(metric Metric, key string) *Attribute {
	for index := range metric.Attributes {
		if metric.Attributes[index].Key == key {
			return &metric.Attributes[index]
		}
	}
	return nil
}

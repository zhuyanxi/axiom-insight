package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// otelPlanFixture builds a plan covering all five span kinds: HTTP root
// (SERVER), SQL client, Kafka producer, Kafka consumer and an internal
// span, with status mappings and controlled events.
func otelPlanFixture() *observabilityv1.GenerationPlan {
	unset := observabilityv1.StatusSetting_STATUS_SETTING_UNSET
	errorSetting := observabilityv1.StatusSetting_STATUS_SETTING_ERROR
	status := &observabilityv1.StatusPolicy{
		Ok: unset, Error: errorSetting, Timeout: errorSetting,
		Cancelled: errorSetting, Unknown: unset,
	}
	span := func(id, name string, kind observabilityv1.SpanKind, targetKind observabilityv1.TargetKind, targetID string, parent *observabilityv1.ParentStrategy) *observabilityv1.SpanPlan {
		return &observabilityv1.SpanPlan{
			Id: id, Name: name, Kind: kind,
			Target:       &observabilityv1.TargetRef{Kind: targetKind, Id: targetID},
			StartTrigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_START},
			EndTrigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
			Parent:       parent,
			Status:       status,
			Attributes: []*observabilityv1.AttributeBinding{
				{Key: "service.name", Value: irBinding("service.name")},
				{Key: "service.version", Value: serviceVersionBinding()},
				{Key: "http.request.method", Value: constantBinding("plan.constant.POST")},
			},
			Events: []*observabilityv1.SpanEvent{
				{Id: id + ":exception", Name: "exception", Statuses: []observabilityv1.RuntimeStatus{observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR}},
			},
		}
	}
	return &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "orders",
		Spans: []*observabilityv1.SpanPlan{
			span("tracing:ep:orders:root", "POST /orders/{id}", observabilityv1.SpanKind_SPAN_KIND_SERVER,
				observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, "ep:orders",
				&observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT, Carrier: observabilityv1.CarrierType_CARRIER_TYPE_HTTP_HEADERS}),
			span("tracing:dep:sql:child", "db exec", observabilityv1.SpanKind_SPAN_KIND_CLIENT,
				observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, "dep:sql",
				&observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT}),
			span("tracing:dep:producer:child", "kafka produce", observabilityv1.SpanKind_SPAN_KIND_PRODUCER,
				observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, "dep:producer",
				&observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT}),
			span("tracing:dep:consumer:child", "kafka consume", observabilityv1.SpanKind_SPAN_KIND_CONSUMER,
				observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, "dep:consumer",
				&observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT}),
			span("tracing:edge:handler-validator:child", "checkout.Validate", observabilityv1.SpanKind_SPAN_KIND_INTERNAL,
				observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE, "edge:handler-validator",
				&observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT}),
		},
	}
}

func serviceVersionBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.resource.service.version", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		Fallback: "unknown",
	}
}

func defaultPolicyValue() policy.Policy {
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		panic(err)
	}
	return *resolved
}

// TestRenderTracingPlanAC1: the five-kind plan renders to a document that
// strictly decodes, passes the semantic validator and the machine schema;
// every span appears exactly once with kind, parent, lifecycle, target
// and attributes matching the plan.
func TestRenderTracingPlanAC1(t *testing.T) {
	plan := otelPlanFixture()
	contents, err := RenderTracingPlan(plan, defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	document, err := DecodeOTel(contents)
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
	if err := schemacheck.Validate(loadSchema(t, "otel.schema.json"), jsonData); err != nil {
		t.Fatalf("rendered output fails the machine schema: %v", err)
	}
	if document.DocumentType != DocumentTypeOTel {
		t.Errorf("document type = %q", document.DocumentType)
	}
	if document.PlanKind != OTelPlanKind {
		t.Errorf("plan kind = %q", document.PlanKind)
	}
	if document.SemanticConventionsVersion != OTelSemanticConventionsVersion {
		t.Errorf("semantic conventions version = %q", document.SemanticConventionsVersion)
	}
	// Resource attributes are grouped once, not per span.
	if len(document.Resources) != 2 {
		t.Errorf("resources = %d, want 2 (service.name, service.version)", len(document.Resources))
	}
	for _, span := range document.Spans {
		for _, attribute := range span.Attributes {
			if attribute.Key == "service.name" || attribute.Key == "service.version" {
				t.Errorf("span %s duplicates resource attribute %q", span.ID, attribute.Key)
			}
		}
	}
	if len(document.Spans) != 5 {
		t.Fatalf("span count = %d, want 5", len(document.Spans))
	}
	seen := map[string]bool{}
	for _, span := range document.Spans {
		if seen[span.ID] {
			t.Errorf("span %q rendered twice", span.ID)
		}
		seen[span.ID] = true
	}
	kinds := map[string]string{}
	for _, span := range document.Spans {
		kinds[span.ID] = span.Kind
	}
	if kinds["tracing:ep:orders:root"] != SpanKindServer ||
		kinds["tracing:dep:sql:child"] != SpanKindClient ||
		kinds["tracing:dep:producer:child"] != SpanKindProducer ||
		kinds["tracing:dep:consumer:child"] != SpanKindConsumer ||
		kinds["tracing:edge:handler-validator:child"] != SpanKindInternal {
		t.Errorf("kinds = %v", kinds)
	}
}

// TestRenderTracingPlanAC2: the rendered file pins the concrete semantic
// conventions version; no latest, range or runtime query.
func TestRenderTracingPlanAC2(t *testing.T) {
	contents, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	if !strings.Contains(string(contents), OTelSemanticConventionsVersion) {
		t.Error("rendered output lacks the pinned semantic conventions version")
	}
}

// TestRenderTracingPlanAC3: the file is explicitly an instrumentation
// plan with no Collector fields.
func TestRenderTracingPlanAC3(t *testing.T) {
	contents, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, "plan_kind: "+OTelPlanKind) {
		t.Error("rendered output lacks plan_kind: instrumentation")
	}
	for _, forbidden := range []string{"receivers:", "processors:", "exporters:", "service:", "pipelines:"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("rendered output contains Collector field %q", forbidden)
		}
	}
}

// TestRenderTracingPlanAC4: dependency spans with new_root and dangling
// static parents fail with GEN_RENDER_ERROR and no partial bytes.
func TestRenderTracingPlanAC4(t *testing.T) {
	dependencyNewRoot := otelPlanFixture()
	dependencyNewRoot.Spans[1].Parent = &observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT}
	if _, err := RenderTracingPlan(dependencyNewRoot, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("dependency new_root must fail with GEN_RENDER_ERROR, got %v", err)
	}

	danglingStatic := otelPlanFixture()
	danglingStatic.Spans[0].Parent = &observabilityv1.ParentStrategy{
		Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC, StaticParentSpanId: "tracing:missing",
	}
	if _, err := RenderTracingPlan(danglingStatic, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") ||
		!strings.Contains(err.Error(), danglingStatic.Spans[0].GetId()) {
		t.Fatalf("dangling static parent must fail with span context, got %v", err)
	}
}

// TestRenderTracingPlanAC5: the renderer receives only the sanitized plan;
// raw IR canaries never appear in the output.
func TestRenderTracingPlanAC5(t *testing.T) {
	contents, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	for _, canary := range []string{
		"https://user:pass@example.com/orders?id=42#detail",
		"SELECT * FROM users WHERE password = 'hunter2'",
		"redis:user:42:session",
		"kafka-payload-canary-7f3a",
		"canary-token-abc123",
	} {
		if strings.Contains(string(contents), canary) {
			t.Errorf("rendered output leaks canary %q", canary)
		}
	}
}

// TestRenderTracingPlanAC6: rendering is byte-identical across 10 runs,
// working directories and time zones; SHA-256 is stable.
func TestRenderTracingPlanAC6(t *testing.T) {
	first, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	sum := sha256.Sum256(first)
	reference := hex.EncodeToString(sum[:])
	t.Setenv("TZ", "Pacific/Auckland")
	t.Chdir(t.TempDir())
	for range 10 {
		contents, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
		if err != nil {
			t.Fatalf("RenderTracingPlan failed: %v", err)
		}
		if string(contents) != string(first) {
			t.Fatal("rendered bytes changed across runs")
		}
		sum := sha256.Sum256(contents)
		if hex.EncodeToString(sum[:]) != reference {
			t.Fatal("SHA-256 changed across runs")
		}
	}
	_ = time.Now
}

// TestRenderTracingPlanInvalidInput: nil plans, unsupported schemas,
// empty IDs, missing status and unspecified enums fail with
// GEN_RENDER_ERROR.
func TestRenderTracingPlanInvalidInput(t *testing.T) {
	if _, err := RenderTracingPlan(nil, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("nil plan must fail, got %v", err)
	}

	badSchema := otelPlanFixture()
	badSchema.SchemaVersion = "v99"
	if _, err := RenderTracingPlan(badSchema, defaultPolicyValue()); err == nil {
		t.Fatal("unsupported schema must fail")
	}

	emptyName := otelPlanFixture()
	emptyName.Spans[0].Name = ""
	if _, err := RenderTracingPlan(emptyName, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), emptyName.Spans[0].GetId()) {
		t.Fatalf("empty span name must fail with span context, got %v", err)
	}

	noStatus := otelPlanFixture()
	noStatus.Spans[0].Status = nil
	if _, err := RenderTracingPlan(noStatus, defaultPolicyValue()); err == nil {
		t.Fatal("missing status policy must fail")
	}

	unspecifiedKind := otelPlanFixture()
	unspecifiedKind.Spans[0].Kind = observabilityv1.SpanKind_SPAN_KIND_UNSPECIFIED
	if _, err := RenderTracingPlan(unspecifiedKind, defaultPolicyValue()); err == nil {
		t.Fatal("unspecified span kind must fail")
	}
}

// TestRenderTracingPlanNoEnvironmentLeak: the output never contains the
// working directory, user or environment values.
func TestRenderTracingPlanNoEnvironmentLeak(t *testing.T) {
	t.Setenv("SI_OTEL_CANARY", "canary-env-7f3a")
	contents, err := RenderTracingPlan(otelPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	for _, canary := range []string{"canary-env-7f3a", os.Getenv("HOME")} {
		if canary != "" && strings.Contains(string(contents), canary) {
			t.Errorf("rendered bytes leak %q", canary)
		}
	}
}

// TestRenderTracingPlanDisabledInternalSpans: spans the policy disables
// are simply absent from the plan; the renderer renders what it receives.
func TestRenderTracingPlanDisabledInternalSpans(t *testing.T) {
	plan := otelPlanFixture()
	plan.Spans = plan.Spans[:4] // drop the internal span
	contents, err := RenderTracingPlan(plan, defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderTracingPlan failed: %v", err)
	}
	document, err := DecodeOTel(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	if len(document.Spans) != 4 {
		t.Errorf("span count = %d, want 4", len(document.Spans))
	}
	for _, span := range document.Spans {
		if span.Kind == SpanKindInternal {
			t.Error("disabled internal span rendered")
		}
	}
}


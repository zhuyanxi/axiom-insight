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

// loggingPlanFixture builds a plan with an endpoint completed/failed pair
// and a dependency failed event, with runtime-context correlation.
func loggingPlanFixture() *observabilityv1.GenerationPlan {
	field := func(key string, value *observabilityv1.ValueBinding) *observabilityv1.FieldBinding {
		return &observabilityv1.FieldBinding{Key: key, Value: value}
	}
	common := []*observabilityv1.FieldBinding{
		field("timestamp", clockBinding()),
		field("event.name", constantFieldValue("plan.constant.http.request.completed")),
		field("service", irFieldValue("service.name")),
		field("module", irFieldValue("function.package_path")),
		field("function", irFieldValue("function.qualified_name")),
		field("operation", constantFieldValue("plan.constant.post")),
		field("version", resourceVersionBinding()),
	}
	return &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "orders",
		Logs: []*observabilityv1.LogPlan{
			{
				Id: "logging:ep:orders:completed", EventName: "http.request.completed",
				Severity: observabilityv1.LogSeverity_LOG_SEVERITY_INFO,
				Target:   &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: "ep:orders"},
				Trigger:  &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
				Statuses: []observabilityv1.RuntimeStatus{observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK},
				CorrelationFields: []string{"request_id", "trace_id", "span_id"},
				RedactionPolicyId: "builtin:v1",
				Fields: append(append([]*observabilityv1.FieldBinding{}, common...),
					field("status", runtimeStatusBinding()),
					field("duration_seconds", runtimeDurationBinding()),
					field("request_id", contextField("runtime.context.request_id", false)),
					field("trace_id", contextField("runtime.context.trace_id", true)),
					field("span_id", contextField("runtime.context.span_id", true)),
				),
			},
			{
				Id: "logging:ep:orders:failed", EventName: "http.request.failed",
				Severity: observabilityv1.LogSeverity_LOG_SEVERITY_ERROR,
				Target:   &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: "ep:orders"},
				Trigger:  &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
				Statuses: []observabilityv1.RuntimeStatus{
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN,
				},
				CorrelationFields: []string{"request_id", "trace_id", "span_id"},
				RedactionPolicyId: "builtin:v1",
				Fields: append(append([]*observabilityv1.FieldBinding{}, common...),
					field("status", runtimeStatusBinding()),
					field("duration_seconds", runtimeDurationBinding()),
					field("error.type", runtimeErrorTypeBinding()),
					field("error.category", runtimeErrorCategoryBinding()),
					field("request_id", contextField("runtime.context.request_id", false)),
					field("trace_id", contextField("runtime.context.trace_id", true)),
					field("span_id", contextField("runtime.context.span_id", true)),
				),
			},
			{
				Id: "logging:dep:sql:failed", EventName: "dependency.operation.failed",
				Severity: observabilityv1.LogSeverity_LOG_SEVERITY_ERROR,
				Target:   &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: "dep:sql"},
				Trigger:  &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
				Statuses: []observabilityv1.RuntimeStatus{
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED,
					observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN,
				},
				RedactionPolicyId: "builtin:v1",
				Fields: append(append([]*observabilityv1.FieldBinding{}, common...),
					field("status", runtimeStatusBinding()),
					field("duration_seconds", runtimeDurationBinding()),
					field("error.type", runtimeErrorTypeBinding()),
					field("error.category", runtimeErrorCategoryBinding()),
					field("system", constantFieldValue("plan.constant.sql")),
					field("request_id", contextField("runtime.context.request_id", false)),
					field("trace_id", contextField("runtime.context.trace_id", false)),
					field("span_id", contextField("runtime.context.span_id", false)),
				),
			},
		},
	}
}

func clockBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.clock", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CLOCK,
		Type: observabilityv1.ValueType_VALUE_TYPE_TIMESTAMP, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func constantFieldValue(path string) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func irFieldValue(path string) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func resourceVersionBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.resource.service.version", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESOURCE,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		Fallback: "unknown",
	}
}

func runtimeStatusBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.operation.status", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STATUS, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

func runtimeDurationBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.operation.duration_seconds", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_DOUBLE, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

func runtimeErrorTypeBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.error.type", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

func runtimeErrorCategoryBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.error.category", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

func contextField(path string, required bool) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		Required: required,
	}
}

// TestRenderLoggingPlanAC1: the complete plan renders to a document that
// strictly decodes, passes the semantic validator and the machine schema;
// every event and field binding matches the plan.
func TestRenderLoggingPlanAC1(t *testing.T) {
	contents, err := RenderLoggingPlan(loggingPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderLoggingPlan failed: %v", err)
	}
	document, err := DecodeLogging(contents)
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
	if err := schemacheck.Validate(loadSchema(t, "logging.schema.json"), jsonData); err != nil {
		t.Fatalf("rendered output fails the machine schema: %v", err)
	}
	if document.DocumentType != DocumentTypeLogging {
		t.Errorf("document type = %q", document.DocumentType)
	}
	if document.SchemaVersion != SchemaVersionLogging {
		t.Errorf("schema version = %q", document.SchemaVersion)
	}
	if len(document.Events) != 3 {
		t.Fatalf("event count = %d, want 3", len(document.Events))
	}
	// Every plan ID appears once, events sorted by ID.
	seen := map[string]bool{}
	previous := ""
	for _, event := range document.Events {
		if seen[event.ID] {
			t.Errorf("event %q rendered twice", event.ID)
		}
		seen[event.ID] = true
		if previous != "" && event.ID < previous {
			t.Errorf("events not sorted by ID")
		}
		previous = event.ID
	}
	for _, logPlan := range loggingPlanFixture().GetLogs() {
		if !seen[logPlan.GetId()] {
			t.Errorf("plan ID %q missing", logPlan.GetId())
		}
	}
	// Conditions are finite statuses in the documented order.
	completed := findLogEvent(document, "logging:ep:orders:completed")
	if completed == nil {
		t.Fatal("completed event missing from rendered document")
	}
	if len(completed.Condition.StatusIn) != 1 || completed.Condition.StatusIn[0] != RuntimeStatusOK {
		t.Errorf("completed condition = %v", completed.Condition.StatusIn)
	}
}

func findLogEvent(document *LoggingDocument, id string) *LogEvent {
	for index := range document.Events {
		if document.Events[index].ID == id {
			return &document.Events[index]
		}
	}
	return nil
}

// TestRenderLoggingPlanAC2: no generated timestamp, random IDs or host
// information ever appears in the file.
func TestRenderLoggingPlanAC2(t *testing.T) {
	contents, err := RenderLoggingPlan(loggingPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderLoggingPlan failed: %v", err)
	}
	text := string(contents)
	for _, fragment := range []string{
		"2026-", "now:", "00000000-0000", "hostname", os.Getenv("HOSTNAME"),
	} {
		if fragment != "" && strings.Contains(text, fragment) {
			t.Errorf("rendered output leaks %q", fragment)
		}
	}
	// The timestamp field is a runtime clock binding, never a value.
	document, _ := DecodeLogging(contents)
	for _, event := range document.Events {
		for _, field := range event.Fields {
			if field.Key == "timestamp" {
				if field.Binding.Source != ValueSourceRuntimeClock {
					t.Errorf("timestamp source = %q", field.Binding.Source)
				}
				if field.Binding.Path == "" {
					t.Error("timestamp must express the runtime binding path")
				}
			}
		}
	}
}

// TestRenderLoggingPlanAC3: the built-in denylist is present and marked
// immutable; user rules only add.
func TestRenderLoggingPlanAC3(t *testing.T) {
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Logging: &policy.LoggingConfig{
			RedactFields: []string{"authorization", "cookie", "password", "secret", "token", "email"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	contents, err := RenderLoggingPlan(loggingPlanFixture(), *resolved)
	if err != nil {
		t.Fatalf("RenderLoggingPlan failed: %v", err)
	}
	document, err := DecodeLogging(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	if !document.Redaction.Immutable {
		t.Error("redaction must be immutable")
	}
	for _, builtin := range []string{"authorization", "cookie", "password", "secret", "token"} {
		if !containsString(document.Redaction.FieldNames, builtin) {
			t.Errorf("built-in denylist entry %q missing", builtin)
		}
	}
	if !containsString(document.Redaction.FieldNames, "email") {
		t.Error("user redaction addition missing")
	}
}

// TestRenderLoggingPlanAC4: overlapping completed/failed conditions fail
// with GEN_RENDER_ERROR and no partial bytes.
func TestRenderLoggingPlanAC4(t *testing.T) {
	plan := loggingPlanFixture()
	plan.Logs[0].Statuses = []observabilityv1.RuntimeStatus{
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
	}
	contents, err := RenderLoggingPlan(plan, defaultPolicyValue())
	if err == nil {
		t.Fatal("overlapping conditions must fail")
	}
	if !strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Errorf("error %q lacks GEN_RENDER_ERROR", err.Error())
	}
	if !strings.Contains(err.Error(), plan.Logs[1].GetId()) {
		t.Errorf("error %q lacks the LogPlan ID", err.Error())
	}
	if len(contents) != 0 {
		t.Error("no partial bytes may be returned")
	}
}

// TestRenderLoggingPlanAC5: canaries never appear in the YAML or error
// messages.
func TestRenderLoggingPlanAC5(t *testing.T) {
	plan := loggingPlanFixture()
	// Inject the canary into the operation constant of the dependency
	// event (index 5 in the common field set).
	plan.Logs[2].Fields[5].GetValue().Path = "plan.constant.Bearer sk-canary-abc123"
	contents, err := RenderLoggingPlan(plan, defaultPolicyValue())
	if err == nil {
		// The constant may be dropped by the scanner with an error; if it
		// renders, the canary must be absent.
		if strings.Contains(string(contents), "sk-canary-abc123") {
			t.Fatal("canary leaked into the rendered file")
		}
	} else {
		if strings.Contains(err.Error(), "sk-canary-abc123") {
			t.Errorf("error message leaks the canary: %v", err)
		}
	}
}

// TestRenderLoggingPlanAC6: rendering is byte-identical across 10 runs,
// working directories and time zones; SHA-256 is stable.
func TestRenderLoggingPlanAC6(t *testing.T) {
	first, err := RenderLoggingPlan(loggingPlanFixture(), defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderLoggingPlan failed: %v", err)
	}
	sum := sha256.Sum256(first)
	reference := hex.EncodeToString(sum[:])
	t.Setenv("TZ", "Asia/Shanghai")
	t.Chdir(t.TempDir())
	for range 10 {
		contents, err := RenderLoggingPlan(loggingPlanFixture(), defaultPolicyValue())
		if err != nil {
			t.Fatalf("RenderLoggingPlan failed: %v", err)
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

// TestRenderLoggingPlanInvalidInput: nil plans, unsupported schemas,
// empty IDs and missing redaction entries fail with GEN_RENDER_ERROR.
func TestRenderLoggingPlanInvalidInput(t *testing.T) {
	if _, err := RenderLoggingPlan(nil, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("nil plan must fail, got %v", err)
	}

	badSchema := loggingPlanFixture()
	badSchema.SchemaVersion = "v99"
	if _, err := RenderLoggingPlan(badSchema, defaultPolicyValue()); err == nil {
		t.Fatal("unsupported schema must fail")
	}

	emptyID := loggingPlanFixture()
	emptyID.Logs[0].Id = ""
	if _, err := RenderLoggingPlan(emptyID, defaultPolicyValue()); err == nil ||
		!strings.Contains(err.Error(), "GEN_RENDER_ERROR") {
		t.Fatalf("empty event ID must fail, got %v", err)
	}

	duplicateField := loggingPlanFixture()
	duplicateField.Logs[0].Fields = append(duplicateField.Logs[0].Fields,
		&observabilityv1.FieldBinding{Key: "trace-id", Value: contextField("runtime.context.trace_id", true)})
	if _, err := RenderLoggingPlan(duplicateField, defaultPolicyValue()); err == nil {
		t.Fatal("normalized duplicate field keys must fail")
	}
}

// TestRenderLoggingPlanUnconditionalStartEvent: a start event (empty
// statuses) renders as firing for every status in the finite order.
func TestRenderLoggingPlanUnconditionalStartEvent(t *testing.T) {
	plan := loggingPlanFixture()
	plan.Logs = plan.Logs[:1]
	plan.Logs[0].EventName = "http.request.started"
	plan.Logs[0].Severity = observabilityv1.LogSeverity_LOG_SEVERITY_INFO
	plan.Logs[0].Statuses = nil
	contents, err := RenderLoggingPlan(plan, defaultPolicyValue())
	if err != nil {
		t.Fatalf("RenderLoggingPlan failed: %v", err)
	}
	document, err := DecodeLogging(contents)
	if err != nil {
		t.Fatalf("re-decode failed: %v", err)
	}
	want := []string{RuntimeStatusOK, RuntimeStatusError, RuntimeStatusCancelled, RuntimeStatusTimeout, RuntimeStatusUnknown}
	if len(document.Events[0].Condition.StatusIn) != 5 {
		t.Fatalf("start condition = %v", document.Events[0].Condition.StatusIn)
	}
	for index, status := range want {
		if document.Events[0].Condition.StatusIn[index] != status {
			t.Errorf("start condition[%d] = %q, want %q", index, document.Events[0].Condition.StatusIn[index], status)
		}
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

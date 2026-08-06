package logging

import (
	"context"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// loggingDocument builds an IR with one HTTP endpoint, one gRPC endpoint,
// one cron endpoint and one dependency of every kind.
func loggingDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:handler", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{
				Id: "ep:http", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:handler",
				HttpMethod: "POST", HttpPath: "/orders/{id}",
			},
			{
				Id: "ep:grpc", Kind: observabilityv1.EndpointKind_GRPC_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:handler",
				GrpcService: "OrderService", GrpcMethod: "CreateOrder",
			},
			{
				Id: "ep:cron", Kind: observabilityv1.EndpointKind_CRON_JOB,
				Name: "Nightly Cleanup", FunctionId: "fn:handler", CronSchedule: "0 3 * * *",
			},
		},
		Dependencies: []*observabilityv1.Dependency{
			{Id: "dep:producer", Kind: observabilityv1.DependencyKind_KAFKA_PRODUCER, Name: "Producer", FunctionId: "fn:handler", Operation: "produce", ValueIsStatic: true},
			{Id: "dep:consumer", Kind: observabilityv1.DependencyKind_KAFKA_CONSUMER, Name: "Consumer", FunctionId: "fn:handler", Operation: "consume", ValueIsStatic: true},
			{Id: "dep:sql", Kind: observabilityv1.DependencyKind_SQL, Name: "Store", FunctionId: "fn:handler", Operation: "exec", ValueIsStatic: true},
			{Id: "dep:redis", Kind: observabilityv1.DependencyKind_REDIS, Name: "Cache", FunctionId: "fn:handler", Operation: "get", ValueIsStatic: true},
			{Id: "dep:http", Kind: observabilityv1.DependencyKind_HTTP_CLIENT, Name: "Client", FunctionId: "fn:handler", Operation: "GET", ValueIsStatic: true},
			{Id: "dep:rpc", Kind: observabilityv1.DependencyKind_RPC_CLIENT, Name: "RPC", FunctionId: "fn:handler", Operation: "CreateOrder", ValueIsStatic: true},
		},
	}
}

func planLogging(t *testing.T, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) *planner.LoggingResult {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	result, err := LoggingPlanner{}.PlanLogging(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: resolved,
	})
	if err != nil {
		t.Fatalf("PlanLogging failed: %v", err)
	}
	return result
}

func eventFor(result *planner.LoggingResult, targetID, purpose string) *observabilityv1.LogPlan {
	for _, event := range result.Items {
		if event.GetTarget().GetId() == targetID && strings.HasSuffix(event.GetId(), ":"+purpose) {
			return event
		}
	}
	return nil
}

func fieldFor(event *observabilityv1.LogPlan, key string) *observabilityv1.ValueBinding {
	for _, field := range event.GetFields() {
		if field.GetKey() == key {
			return field.GetValue()
		}
	}
	return nil
}

// TestConditionsMutuallyExclusiveAC1: ok fires only completed;
// error/timeout/cancelled fire only failed — no double logging.
func TestConditionsMutuallyExclusiveAC1(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	completed := eventFor(result, "ep:http", planner.PurposeEnd)
	failed := eventFor(result, "ep:http", planner.PurposeFailed)
	if completed == nil || failed == nil {
		t.Fatalf("missing endpoint events: completed=%v failed=%v", completed, failed)
	}
	if len(completed.GetStatuses()) != 1 || completed.GetStatuses()[0] != observabilityv1.RuntimeStatus_RUNTIME_STATUS_OK {
		t.Errorf("completed statuses = %v, want [ok]", completed.GetStatuses())
	}
	wantFailed := []observabilityv1.RuntimeStatus{
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED,
		observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNKNOWN,
	}
	if len(failed.GetStatuses()) != len(wantFailed) {
		t.Fatalf("failed statuses = %v", failed.GetStatuses())
	}
	for index, status := range wantFailed {
		if failed.GetStatuses()[index] != status {
			t.Errorf("failed statuses[%d] = %v, want %v", index, failed.GetStatuses()[index], status)
		}
	}
	// No status belongs to both sets.
	for _, status := range completed.GetStatuses() {
		for _, failedStatus := range failed.GetStatuses() {
			if status == failedStatus {
				t.Errorf("status %v matches both completed and failed", status)
			}
		}
	}
}

// TestNoStartEventsByDefaultAC2: defaults emit completed/failed only;
// enabling emit_start_events adds exactly one started event per endpoint.
func TestNoStartEventsByDefaultAC2(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	for _, endpointID := range []string{"ep:http", "ep:grpc", "ep:cron"} {
		if eventFor(result, endpointID, planner.PurposeStart) != nil {
			t.Errorf("start event for %s must be absent by default", endpointID)
		}
		if eventFor(result, endpointID, planner.PurposeEnd) == nil || eventFor(result, endpointID, planner.PurposeFailed) == nil {
			t.Errorf("completed/failed events missing for %s", endpointID)
		}
	}
	// Start events never carry duration or status.
	enabled, _ := policy.Resolve(&policy.GenerationConfig{
		Logging: &policy.LoggingConfig{EmitStartEvents: new(true)},
	}, nil)
	result = planLogging(t, document, *enabled)
	for _, endpointID := range []string{"ep:http", "ep:grpc", "ep:cron"} {
		started := eventFor(result, endpointID, planner.PurposeStart)
		if started == nil {
			t.Fatalf("start event missing for %s when enabled", endpointID)
		}
		if fieldFor(started, "status") != nil || fieldFor(started, "duration_seconds") != nil {
			t.Errorf("start event for %s carries result fields", endpointID)
		}
	}
}

// TestCorrelationSourcesAC3: request_id/trace_id/span_id are runtime
// context bindings, never static values or generator IDs.
func TestCorrelationSourcesAC3(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	completed := eventFor(result, "ep:http", planner.PurposeEnd)
	if completed == nil {
		t.Fatal("missing completed event")
	}
	for _, key := range []string{"request_id", "trace_id", "span_id"} {
		binding := fieldFor(completed, key)
		if binding == nil {
			t.Fatalf("missing correlation field %s", key)
		}
		if binding.GetSource() != observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CONTEXT {
			t.Errorf("%s source = %v, want runtime_context", key, binding.GetSource())
		}
		if !strings.HasPrefix(binding.GetPath(), "runtime.context.") {
			t.Errorf("%s path = %q", key, binding.GetPath())
		}
		if binding.GetPath() == "runtime.context.trace_id" && !binding.GetRequired() {
			t.Errorf("trace_id must be required under a root span")
		}
		if key == "request_id" && binding.GetRequired() {
			t.Errorf("request_id must stay optional")
		}
	}
}

// TestDependencyCorrelationOptionalAC4: dependency failed events carry
// optional trace/span bindings; no unknown or random placeholders.
func TestDependencyCorrelationOptionalAC4(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	failed := eventFor(result, "dep:sql", planner.PurposeFailed)
	if failed == nil {
		t.Fatal("missing dependency failed event")
	}
	for _, key := range []string{"trace_id", "span_id"} {
		binding := fieldFor(failed, key)
		if binding == nil {
			t.Fatalf("missing %s on dependency event", key)
		}
		if binding.GetRequired() {
			t.Errorf("%s must be optional without a provable root span", key)
		}
	}
	for _, field := range failed.GetFields() {
		if field.GetKey() == "trace_id" || field.GetKey() == "span_id" {
			if binding := field.GetValue(); binding.GetFallback() == "unknown" || binding.GetFallback() != "" {
				t.Errorf("correlation field %s must not carry a fake fallback: %q", field.GetKey(), binding.GetFallback())
			}
		}
	}
}

// TestSensitiveFieldsNeverEnterPlanAC5: credential, SQL, Redis value and
// payload canaries never appear in fields, constants or diagnostics.
func TestSensitiveFieldsNeverEnterPlanAC5(t *testing.T) {
	document := loggingDocument()
	document.Dependencies[2].Operation = "SELECT * FROM users WHERE password = 'hunter2'"
	document.Dependencies[3].Operation = "redis:user:42:session"
	document.Dependencies[4].Operation = "https://user:pass@example.com/orders?id=42"
	document.Dependencies[0].Operation = "Bearer sk-canary-token-abc123"
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	var all string
	for _, event := range result.Items {
		all += event.GetEventName() + " "
		for _, field := range event.GetFields() {
			all += field.GetKey() + " " + field.GetValue().GetPath() + " "
		}
	}
	for _, diagnostic := range result.Diagnostics {
		all += diagnostic.Message + " "
	}
	for _, canary := range []string{"hunter2", "user:42:session", "user:pass@example.com", "sk-canary-token-abc123"} {
		if strings.Contains(all, canary) {
			t.Fatalf("plan leaks canary %q: %s", canary, all)
		}
	}
	// The blocked operations must produce locatable, value-free
	// diagnostics.
	sensitiveFound := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeSensitiveValueDropped {
			sensitiveFound = true
		}
	}
	if !sensitiveFound {
		t.Errorf("expected GEN_SENSITIVE_VALUE_DROPPED diagnostics: %v", result.Diagnostics)
	}
}

// TestDependencyLogVolumeAC6: by default only failure results match a
// dependency log event; successful calls produce none.
func TestDependencyLogVolumeAC6(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	for _, dependencyID := range []string{"dep:producer", "dep:consumer", "dep:sql", "dep:redis", "dep:http", "dep:rpc"} {
		event := eventFor(result, dependencyID, planner.PurposeFailed)
		if event == nil {
			t.Fatalf("dependency %s must have exactly one failed event", dependencyID)
		}
		if event.GetEventName() != dependencyFailedEvent {
			t.Errorf("dependency %s event name = %q", dependencyID, event.GetEventName())
		}
		if eventFor(result, dependencyID, planner.PurposeEnd) != nil {
			t.Errorf("dependency %s must not have a completion event", dependencyID)
		}
	}
}

// TestSeverityMappingAC7: every controlled status maps to one documented
// severity; unknown strings cannot bypass the enum.
func TestSeverityMappingAC7(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	completed := eventFor(result, "ep:http", planner.PurposeEnd)
	failed := eventFor(result, "ep:http", planner.PurposeFailed)
	if completed.GetSeverity() != observabilityv1.LogSeverity_LOG_SEVERITY_INFO {
		t.Errorf("completed severity = %v, want INFO", completed.GetSeverity())
	}
	if failed.GetSeverity() != observabilityv1.LogSeverity_LOG_SEVERITY_ERROR {
		t.Errorf("failed severity = %v, want ERROR", failed.GetSeverity())
	}
	// Dependency failed events use the same ERROR mapping.
	depFailed := eventFor(result, "dep:sql", planner.PurposeFailed)
	if depFailed.GetSeverity() != observabilityv1.LogSeverity_LOG_SEVERITY_ERROR {
		t.Errorf("dependency failed severity = %v", depFailed.GetSeverity())
	}
	// severityForStatus table is exhaustive over the finite vocabulary.
	if severityForStatus(observabilityv1.RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED) != observabilityv1.LogSeverity_LOG_SEVERITY_ERROR {
		t.Error("unspecified status must fall into the controlled default")
	}
}

// TestPolicyMatrix: completion events and dependency errors respond to
// the policy.
func TestPolicyMatrix(t *testing.T) {
	document := loggingDocument()
	disabled, _ := policy.Resolve(&policy.GenerationConfig{
		Logging: &policy.LoggingConfig{
			EmitCompletionEvents: new(false),
			EmitDependencyErrors: new(false),
		},
	}, nil)
	result := planLogging(t, document, *disabled)
	// emit_completion_events governs both completed and failed endpoint
	// events.
	for _, endpointID := range []string{"ep:http", "ep:grpc", "ep:cron"} {
		if eventFor(result, endpointID, planner.PurposeEnd) != nil {
			t.Errorf("completed event for %s must be disabled", endpointID)
		}
		if eventFor(result, endpointID, planner.PurposeFailed) != nil {
			t.Errorf("failed event for %s must be disabled", endpointID)
		}
	}
	for _, dependencyID := range []string{"dep:sql", "dep:redis"} {
		if eventFor(result, dependencyID, planner.PurposeFailed) != nil {
			t.Errorf("dependency failed event for %s must be disabled", dependencyID)
		}
	}
}

// TestCorrelationFieldsPolicy: correlation fields are restricted to the
// configured subset of the allowlist.
func TestCorrelationFieldsPolicy(t *testing.T) {
	document := loggingDocument()
	restricted, _ := policy.Resolve(&policy.GenerationConfig{
		Logging: &policy.LoggingConfig{CorrelationFields: []string{"trace_id"}},
	}, nil)
	result := planLogging(t, document, *restricted)
	completed := eventFor(result, "ep:http", planner.PurposeEnd)
	if completed == nil {
		t.Fatal("missing completed event")
	}
	if fieldFor(completed, "request_id") != nil || fieldFor(completed, "span_id") != nil {
		t.Error("unconfigured correlation fields must be absent")
	}
	if fieldFor(completed, "trace_id") == nil {
		t.Error("configured correlation field must be present")
	}
}

// TestEndpointKindFields: HTTP carries method/route, gRPC service/method,
// cron the stable job name.
func TestEndpointKindFields(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	httpCompleted := eventFor(result, "ep:http", planner.PurposeEnd)
	if method := fieldFor(httpCompleted, "method"); method == nil || method.GetPath() != "plan.constant.POST" {
		t.Errorf("http method field = %v", method)
	}
	if route := fieldFor(httpCompleted, "route"); route == nil || route.GetPath() != "endpoint.http_path" {
		t.Errorf("http route field = %v", route)
	}
	grpcCompleted := eventFor(result, "ep:grpc", planner.PurposeEnd)
	if fieldFor(grpcCompleted, "rpc.service") == nil || fieldFor(grpcCompleted, "rpc.method") == nil {
		t.Error("gRPC service/method fields missing")
	}
	cronCompleted := eventFor(result, "ep:cron", planner.PurposeEnd)
	if job := fieldFor(cronCompleted, "cron.job.name"); job == nil || job.GetPath() != "endpoint.name" {
		t.Errorf("cron job field = %v", job)
	}
	depFailed := eventFor(result, "dep:sql", planner.PurposeFailed)
	if system := fieldFor(depFailed, "system"); system == nil || system.GetPath() != "plan.constant.sql" {
		t.Errorf("dependency system field = %v", system)
	}
}

// TestErrorFieldsLimited: error fields are exactly type and category;
// message and stacktrace are never bound.
func TestErrorFieldsLimited(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	failed := eventFor(result, "ep:http", planner.PurposeFailed)
	for _, key := range []string{"error.type", "error.category"} {
		if fieldFor(failed, key) == nil {
			t.Errorf("missing error field %s", key)
		}
	}
	for _, key := range []string{"error.message", "error.stacktrace"} {
		if fieldFor(failed, key) != nil {
			t.Errorf("error field %s must never be bound", key)
		}
	}
}

// TestUnknownKindSkipped: unknown endpoint and dependency kinds produce
// no events.
func TestUnknownKindSkipped(t *testing.T) {
	document := loggingDocument()
	document.Endpoints[0].Kind = observabilityv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED
	document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
		Id: "dep:unknown", Kind: observabilityv1.DependencyKind_DEPENDENCY_KIND_UNSPECIFIED,
		FunctionId: "fn:handler", Operation: "mystery", ValueIsStatic: true,
	})
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	if result.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", result.Skipped)
	}
	found := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeUnsupportedEntity {
			found++
		}
	}
	if found != 2 {
		t.Errorf("GEN_UNSUPPORTED_ENTITY count = %d, want 2", found)
	}
}

// TestTimestampIsRuntimeClock: the timestamp field is a runtime clock
// binding; the generator never fills a concrete time.
func TestTimestampIsRuntimeClock(t *testing.T) {
	document := loggingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planLogging(t, document, *defaults)
	completed := eventFor(result, "ep:http", planner.PurposeEnd)
	timestamp := fieldFor(completed, "timestamp")
	if timestamp == nil {
		t.Fatal("missing timestamp field")
	}
	if timestamp.GetSource() != observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_CLOCK {
		t.Errorf("timestamp source = %v, want runtime_clock", timestamp.GetSource())
	}
	if timestamp.GetType() != observabilityv1.ValueType_VALUE_TYPE_TIMESTAMP {
		t.Errorf("timestamp type = %v, want timestamp", timestamp.GetType())
	}
}

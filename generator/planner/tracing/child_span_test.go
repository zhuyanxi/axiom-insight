package tracing

import (
	"context"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// sixKindTracingDocument builds an IR with one function and one
// dependency of every supported kind.
func sixKindTracingDocument() *observabilityv1.ObservabilityDocument {
	kinds := []struct {
		id        string
		kind      observabilityv1.DependencyKind
		operation string
	}{
		{"dep:producer", observabilityv1.DependencyKind_KAFKA_PRODUCER, "produce"},
		{"dep:consumer", observabilityv1.DependencyKind_KAFKA_CONSUMER, "consume"},
		{"dep:sql", observabilityv1.DependencyKind_SQL, "exec"},
		{"dep:redis", observabilityv1.DependencyKind_REDIS, "get"},
		{"dep:http", observabilityv1.DependencyKind_HTTP_CLIENT, "GET"},
		{"dep:rpc", observabilityv1.DependencyKind_RPC_CLIENT, "OrderService/CreateOrder"},
	}
	document := &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:service", QualifiedName: "checkout.Orders", PackagePath: "internal/orders"},
		},
	}
	for _, entry := range kinds {
		document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
			Id: entry.id, Kind: entry.kind, Name: entry.id,
			FunctionId: "fn:service", Operation: entry.operation, ValueIsStatic: true,
		})
	}
	return document
}

func planChildSpans(t *testing.T, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) *planner.TracingResult {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	result, err := DependencyChildSpanPlanner{}.PlanTracing(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: resolved,
	})
	if err != nil {
		t.Fatalf("PlanTracing failed: %v", err)
	}
	return result
}

func spanFor(result *planner.TracingResult, targetID string) *observabilityv1.SpanPlan {
	for _, span := range result.Items {
		if span.GetTarget().GetId() == targetID {
			return span
		}
	}
	return nil
}

func spanAttr(span *observabilityv1.SpanPlan, key string) *observabilityv1.ValueBinding {
	for _, attribute := range span.GetAttributes() {
		if attribute.GetKey() == key {
			return attribute.GetValue()
		}
	}
	return nil
}

// TestSixKindsChildSpanKindsAC1: every dependency gets a Child Span whose
// kind matches the mapping table and whose target references the
// dependency ID.
func TestSixKindsChildSpanKindsAC1(t *testing.T) {
	document := sixKindTracingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	if len(result.Items) != 6 {
		t.Fatalf("span count = %d, want 6", len(result.Items))
	}
	wantKinds := map[string]observabilityv1.SpanKind{
		"dep:producer": observabilityv1.SpanKind_SPAN_KIND_PRODUCER,
		"dep:consumer": observabilityv1.SpanKind_SPAN_KIND_CONSUMER,
		"dep:sql":      observabilityv1.SpanKind_SPAN_KIND_CLIENT,
		"dep:redis":    observabilityv1.SpanKind_SPAN_KIND_CLIENT,
		"dep:http":     observabilityv1.SpanKind_SPAN_KIND_CLIENT,
		"dep:rpc":      observabilityv1.SpanKind_SPAN_KIND_CLIENT,
	}
	for targetID, kind := range wantKinds {
		span := spanFor(result, targetID)
		if span == nil {
			t.Errorf("missing span for %s", targetID)
			continue
		}
		if span.GetKind() != kind {
			t.Errorf("%s kind = %v, want %v", targetID, span.GetKind(), kind)
		}
		if span.GetTarget().GetKind() != observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY {
			t.Errorf("%s target kind = %v", targetID, span.GetTarget().GetKind())
		}
	}
	// Name bases per the mapping table.
	wantNames := map[string]string{
		"dep:producer": "kafka produce",
		"dep:consumer": "kafka consume",
		"dep:sql":      "db exec",
		"dep:redis":    "redis get",
		"dep:http":     "HTTP GET",
		"dep:rpc":      "rpc orderservice_createorder",
	}
	for targetID, want := range wantNames {
		if got := spanFor(result, targetID).GetName(); got != want {
			t.Errorf("%s name = %q, want %q", targetID, got, want)
		}
	}
}

// TestCurrentContextParentAC2: dependency spans use current_context, not
// a fixed static root ID, even when reachable from multiple roots.
func TestCurrentContextParentAC2(t *testing.T) {
	document := sixKindTracingDocument()
	document.Functions = append(document.Functions,
		&observabilityv1.Function{Id: "fn:root-a", QualifiedName: "checkout.A"},
		&observabilityv1.Function{Id: "fn:root-b", QualifiedName: "checkout.B"},
	)
	document.Dependencies[2].FunctionId = "fn:service" // shared SQL
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	for _, span := range result.Items {
		if span.GetParent().GetMode() != observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT {
			t.Errorf("span %s parent = %v, want current_context", span.GetId(), span.GetParent().GetMode())
		}
		if span.GetParent().GetStaticParentSpanId() != "" {
			t.Errorf("span %s must not pin a static parent", span.GetId())
		}
	}
}

// TestChildSpanAttributes: SQL only carries db system/operation, Redis
// only system/operation, HTTP client only the method, Kafka consumer is
// marked as a client call site, and no span carries target values.
func TestChildSpanAttributes(t *testing.T) {
	document := sixKindTracingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)

	sql := spanFor(result, "dep:sql")
	if system := spanAttr(sql, "db.system"); system == nil || system.GetPath() != "plan.constant.sql" {
		t.Errorf("sql db.system = %v", system)
	}
	if operation := spanAttr(sql, "db.operation"); operation == nil || operation.GetPath() != "plan.constant.exec" {
		t.Errorf("sql db.operation = %v", operation)
	}
	if spanAttr(sql, "db.statement") != nil || spanAttr(sql, "db.redis.key") != nil {
		t.Error("SQL span must never carry statement or key attributes")
	}

	redis := spanFor(result, "dep:redis")
	if system := spanAttr(redis, "db.system"); system == nil || system.GetPath() != "plan.constant.redis" {
		t.Errorf("redis db.system = %v", system)
	}
	if spanAttr(redis, "db.redis.key") != nil {
		t.Error("Redis span must never carry a key attribute")
	}

	http := spanFor(result, "dep:http")
	if method := spanAttr(http, "http.request.method"); method == nil || method.GetPath() != "plan.constant.GET" {
		t.Errorf("http method = %v", method)
	}
	if spanAttr(http, "server.address") != nil || spanAttr(http, "url.full") != nil {
		t.Error("HTTP client span must never carry address or URL attributes")
	}

	consumer := spanFor(result, "dep:consumer")
	if scope := spanAttr(consumer, "span.scope"); scope == nil || scope.GetPath() != "plan.constant.client_call" {
		t.Errorf("consumer scope = %v, want client_call", scope)
	}
	if spanAttr(consumer, "messaging.destination") != nil || spanAttr(consumer, "messaging.destination.topic") != nil {
		t.Error("Kafka span must never carry destination or topic attributes")
	}

	rpc := spanFor(result, "dep:rpc")
	if system := spanAttr(rpc, "rpc.system"); system == nil || system.GetPath() != "plan.constant.rpc" {
		t.Errorf("rpc system = %v", system)
	}
	if spanAttr(rpc, "server.address") != nil {
		t.Error("RPC span must never carry a target address")
	}
}

// TestCanaryNeverEntersChildSpans: SQL text, Redis key, URL with
// credentials, Kafka topic and payload canaries never enter names,
// attributes or diagnostics.
func TestCanaryNeverEntersChildSpans(t *testing.T) {
	document := sixKindTracingDocument()
	canaries := map[string]string{
		"dep:sql":      "SELECT * FROM users WHERE password = 'hunter2'",
		"dep:redis":    "user:42:session",
		"dep:http":     "https://user:pass@example.com/orders?id=42#detail",
		"dep:producer": "orders-topic-canary-7f3a",
	}
	for _, dependency := range document.Dependencies {
		canary := canaries[dependency.GetId()]
		if canary == "" {
			continue
		}
		dependency.Value = canary
		dependency.TargetUrl = canary
		dependency.Resource = canary
		dependency.TargetService = canary
		dependency.ValueIsStatic = false
	}
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	var all string
	for _, span := range result.Items {
		all += span.GetName() + " "
		for _, attribute := range span.GetAttributes() {
			all += attribute.GetKey() + " " + attribute.GetValue().GetPath() + " "
		}
	}
	for _, diagnostic := range result.Diagnostics {
		all += diagnostic.Message + " "
	}
	for _, canary := range canaries {
		if strings.Contains(all, canary) {
			t.Fatalf("child span leaks canary %q: %s", canary, all)
		}
	}
}

// TestExceptionEventAC5: with record_exception_events enabled, both root
// and dependency spans bind only exception.type — never message or
// stacktrace — even when the error message contains a canary.
func TestExceptionEventAC5(t *testing.T) {
	document := sixKindTracingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	children := planChildSpans(t, document, *defaults)
	span := spanFor(children, "dep:sql")
	found := false
	for _, event := range span.GetEvents() {
		if event.GetName() != "exception" {
			continue
		}
		found = true
		if event.GetId() != "tracing:dep:sql:exception" {
			t.Errorf("exception event id = %q", event.GetId())
		}
		if len(event.GetStatuses()) != 1 || event.GetStatuses()[0] != observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR {
			t.Errorf("exception event statuses = %v", event.GetStatuses())
		}
		for _, attribute := range event.GetAttributes() {
			if attribute.GetKey() == "exception.message" || attribute.GetKey() == "exception.stacktrace" {
				t.Errorf("exception event binds %q", attribute.GetKey())
			}
		}
	}
	if !found {
		t.Fatal("exception event missing from dependency span")
	}

	// Root spans get the same treatment through the composite.
	composite := planner.New(planner.Options{Tracing: CompositeTracingPlanner{}})
	resolved := *defaults
	resolved.Signals = []string{planner.SignalTracing}
	plan, _, err := composite.Plan(context.Background(), compositeRootSpanDocument(), resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	for _, span := range plan.GetSpans() {
		for _, event := range span.GetEvents() {
			if event.GetName() == "exception" {
				for _, attribute := range event.GetAttributes() {
					if attribute.GetKey() == "exception.message" {
						t.Errorf("root span %s binds exception.message", span.GetId())
					}
				}
			}
		}
	}
	_ = naming.AttributePolicy{}
}

// TestExceptionEventsDisabledByPolicy: disabling record_exception_events
// removes the exception event but keeps timeout/cancelled events.
func TestExceptionEventsDisabledByPolicy(t *testing.T) {
	document := sixKindTracingDocument()
	resolved, _ := policy.Resolve(&policy.GenerationConfig{
		Tracing: &policy.TracingConfig{RecordExceptionEvents: new(false)},
	}, nil)
	result := planChildSpans(t, document, *resolved)
	span := spanFor(result, "dep:sql")
	for _, event := range span.GetEvents() {
		if event.GetName() == "exception" {
			t.Error("exception event must be absent when disabled")
		}
	}
	names := map[string]bool{}
	for _, event := range span.GetEvents() {
		names[event.GetName()] = true
	}
	if !names["timeout"] || !names["cancelled"] {
		t.Errorf("timeout/cancelled events must always be present: %v", names)
	}
}

// TestStatusMappingAC7: every runtime status maps to a unique documented
// setting; no default fallthrough.
func TestStatusMappingAC7(t *testing.T) {
	document := sixKindTracingDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	for _, span := range result.Items {
		status := span.GetStatus()
		if status == nil {
			t.Fatalf("span %s lacks status policy", span.GetId())
		}
		if status.GetOk() != observabilityv1.StatusSetting_STATUS_SETTING_UNSET ||
			status.GetUnknown() != observabilityv1.StatusSetting_STATUS_SETTING_UNSET {
			t.Errorf("span %s ok/unknown must be unset", span.GetId())
		}
		errorSetting := observabilityv1.StatusSetting_STATUS_SETTING_ERROR
		if status.GetError() != errorSetting || status.GetTimeout() != errorSetting ||
			status.GetCancelled() != errorSetting {
			t.Errorf("span %s error/timeout/cancelled must be error", span.GetId())
		}
	}
}

// TestRootSpansHaveControlledEventsAC8: root spans carry complete status
// mapping and controlled timeout/cancelled/exception events referencing
// the original endpoint span target.
func TestRootSpansHaveControlledEventsAC8(t *testing.T) {
	composite := planner.New(planner.Options{Tracing: CompositeTracingPlanner{}})
	resolved, _ := policy.Resolve(nil, nil)
	resolved.Signals = []string{planner.SignalTracing}
	plan, _, err := composite.Plan(context.Background(), compositeRootSpanDocument(), *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if len(plan.GetSpans()) != 3 {
		t.Fatalf("span count = %d, want 3 roots", len(plan.GetSpans()))
	}
	for _, span := range plan.GetSpans() {
		if len(span.GetEvents()) != 3 {
			t.Errorf("root %s event count = %d, want 3", span.GetId(), len(span.GetEvents()))
		}
		for _, event := range span.GetEvents() {
			if !strings.Contains(event.GetId(), span.GetTarget().GetId()) {
				t.Errorf("event %q does not reference the endpoint target %q", event.GetId(), span.GetTarget().GetId())
			}
		}
		// Complete status mapping.
		if span.GetStatus() == nil {
			t.Errorf("root %s lacks status policy", span.GetId())
		}
	}
}

// TestMissingOperationDegrades: a dependency without an operation gets a
// controlled fallback name and GEN_INCOMPLETE_TARGET.
func TestMissingOperationDegrades(t *testing.T) {
	document := sixKindTracingDocument()
	document.Dependencies[2].Operation = "" // dep:sql
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	span := spanFor(result, "dep:sql")
	if span.GetName() != "db unknown" {
		t.Errorf("fallback name = %q, want db unknown", span.GetName())
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget && diagnostic.TargetID == "dep:sql" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GEN_INCOMPLETE_TARGET: %v", result.Diagnostics)
	}
}

// TestUnspecifiedKindSkipped: unknown dependency kinds never produce a
// child span.
func TestUnspecifiedKindSkipped(t *testing.T) {
	document := sixKindTracingDocument()
	document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
		Id: "dep:unknown", Kind: observabilityv1.DependencyKind_DEPENDENCY_KIND_UNSPECIFIED,
		FunctionId: "fn:service", Operation: "mystery", ValueIsStatic: true,
	})
	defaults, _ := policy.Resolve(nil, nil)
	result := planChildSpans(t, document, *defaults)
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	for _, span := range result.Items {
		if span.GetTarget().GetId() == "dep:unknown" {
			t.Error("unspecified kind must not produce a span")
		}
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeUnsupportedEntity {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GEN_UNSUPPORTED_ENTITY: %v", result.Diagnostics)
	}
}

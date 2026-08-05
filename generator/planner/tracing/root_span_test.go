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

// rootSpanDocument builds an IR with one function and one endpoint of the
// given kind.
func rootSpanDocument(kind observabilityv1.EndpointKind) *observabilityv1.ObservabilityDocument {
	endpoint := &observabilityv1.Endpoint{
		Id: "ep:orders", Kind: kind, Name: "CreateOrder", FunctionId: "fn:handler",
	}
	switch kind {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		endpoint.HttpMethod = "POST"
		endpoint.HttpPath = "/orders/{id}"
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		endpoint.GrpcService = "OrderService"
		endpoint.GrpcMethod = "CreateOrder"
	case observabilityv1.EndpointKind_CRON_JOB:
		endpoint.CronSchedule = "0 3 * * *"
	}
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:handler", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
		},
		Endpoints: []*observabilityv1.Endpoint{endpoint},
	}
}

func planRootSpans(t *testing.T, document *observabilityv1.ObservabilityDocument) *planner.TracingResult {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	result, err := EndpointRootSpanPlanner{}.PlanTracing(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: *resolved,
	})
	if err != nil {
		t.Fatalf("PlanTracing failed: %v", err)
	}
	return result
}

func spanAttribute(span *observabilityv1.SpanPlan, key string) *observabilityv1.ValueBinding {
	for _, attribute := range span.GetAttributes() {
		if attribute.GetKey() == key {
			return attribute.GetValue()
		}
	}
	return nil
}

// TestHTTPRootSpanAC1: POST /orders/{id} yields one SERVER root span with
// the controlled method+route name and extract_or_root over HTTP headers.
func TestHTTPRootSpanAC1(t *testing.T) {
	result := planRootSpans(t, rootSpanDocument(observabilityv1.EndpointKind_HTTP_HANDLER))
	if len(result.Items) != 1 {
		t.Fatalf("span count = %d, want 1", len(result.Items))
	}
	span := result.Items[0]
	if span.GetName() != "POST /orders/{id}" {
		t.Errorf("name = %q", span.GetName())
	}
	if span.GetKind() != observabilityv1.SpanKind_SPAN_KIND_SERVER {
		t.Errorf("kind = %v, want SERVER", span.GetKind())
	}
	if span.GetId() != "tracing:ep:orders:root" {
		t.Errorf("id = %q", span.GetId())
	}
	if span.GetParent().GetMode() != observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_EXTRACT_OR_ROOT {
		t.Errorf("parent mode = %v", span.GetParent().GetMode())
	}
	if span.GetParent().GetCarrier() != observabilityv1.CarrierType_CARRIER_TYPE_HTTP_HEADERS {
		t.Errorf("carrier = %v", span.GetParent().GetCarrier())
	}
	if span.GetStartTrigger().GetPhase() != observabilityv1.TriggerPhase_TRIGGER_PHASE_START ||
		span.GetEndTrigger().GetPhase() != observabilityv1.TriggerPhase_TRIGGER_PHASE_END {
		t.Errorf("lifecycle triggers wrong: %v %v", span.GetStartTrigger(), span.GetEndTrigger())
	}
	method := spanAttribute(span, "http.request.method")
	if method == nil || method.GetPath() != "plan.constant.POST" {
		t.Errorf("method attribute = %v", method)
	}
	route := spanAttribute(span, "http.route")
	if route == nil || route.GetPath() != "endpoint.http_path" {
		t.Errorf("route attribute = %v", route)
	}
}

// TestGRPCRootSpanAC2: the gRPC root carries rpc system/service/method
// and no payload attributes.
func TestGRPCRootSpanAC2(t *testing.T) {
	result := planRootSpans(t, rootSpanDocument(observabilityv1.EndpointKind_GRPC_HANDLER))
	span := result.Items[0]
	if span.GetName() != "OrderService/CreateOrder" {
		t.Errorf("name = %q", span.GetName())
	}
	if span.GetKind() != observabilityv1.SpanKind_SPAN_KIND_SERVER {
		t.Errorf("kind = %v", span.GetKind())
	}
	if span.GetParent().GetCarrier() != observabilityv1.CarrierType_CARRIER_TYPE_GRPC_METADATA {
		t.Errorf("carrier = %v", span.GetParent().GetCarrier())
	}
	if system := spanAttribute(span, "rpc.system"); system == nil || system.GetPath() != "plan.constant.grpc" {
		t.Errorf("rpc.system = %v", system)
	}
	if service := spanAttribute(span, "rpc.service"); service == nil || service.GetPath() != "endpoint.grpc_service" {
		t.Errorf("rpc.service = %v", service)
	}
	if method := spanAttribute(span, "rpc.method"); method == nil || method.GetPath() != "endpoint.grpc_method" {
		t.Errorf("rpc.method = %v", method)
	}
	for _, attribute := range span.GetAttributes() {
		if strings.Contains(attribute.GetKey(), "payload") ||
			strings.Contains(attribute.GetKey(), "request") && attribute.GetKey() != "http.request.method" {
			t.Errorf("gRPC span carries payload-ish attribute %q", attribute.GetKey())
		}
	}
}

// TestCronRootSpanAC3: the cron root is INTERNAL with new_root, a stable
// job identity and the static schedule.
func TestCronRootSpanAC3(t *testing.T) {
	result := planRootSpans(t, rootSpanDocument(observabilityv1.EndpointKind_CRON_JOB))
	span := result.Items[0]
	if span.GetName() != "cron CreateOrder" {
		t.Errorf("name = %q", span.GetName())
	}
	if span.GetKind() != observabilityv1.SpanKind_SPAN_KIND_INTERNAL {
		t.Errorf("kind = %v, want INTERNAL", span.GetKind())
	}
	if span.GetParent().GetMode() != observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT {
		t.Errorf("parent mode = %v, want new_root", span.GetParent().GetMode())
	}
	if span.GetParent().GetCarrier() != observabilityv1.CarrierType_CARRIER_TYPE_NONE {
		t.Errorf("carrier = %v, want none", span.GetParent().GetCarrier())
	}
	if job := spanAttribute(span, "cron.job.name"); job == nil || job.GetPath() != "endpoint.name" {
		t.Errorf("cron.job.name = %v", job)
	}
	if schedule := spanAttribute(span, "cron.job.schedule"); schedule == nil || schedule.GetPath() != "endpoint.cron_schedule" {
		t.Errorf("cron.job.schedule = %v", schedule)
	}
}

// TestNoKafkaHandlerRootAC4: a document with only dependencies never
// gets a root span; Kafka consumers are left to the dependency rules.
func TestNoKafkaHandlerRootAC4(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints = nil
	document.Dependencies = []*observabilityv1.Dependency{
		{
			Id: "dep:consumer", Kind: observabilityv1.DependencyKind_KAFKA_CONSUMER,
			Name: "Consumer", FunctionId: "fn:handler", Operation: "consume",
		},
	}
	result := planRootSpans(t, document)
	if len(result.Items) != 0 {
		t.Fatalf("no root span may be created for dependencies, got %d", len(result.Items))
	}
}

// TestRootSpanCanaryAC5: a dependency target URL carrying userinfo/query
// canaries never enters the root span name, attributes or diagnostics —
// root planning only touches endpoints, so the canary stays out.
func TestRootSpanCanaryAC5(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Dependencies = []*observabilityv1.Dependency{
		{
			Id: "dep:http", Kind: observabilityv1.DependencyKind_HTTP_CLIENT,
			Name: "OrdersClient", FunctionId: "fn:handler",
			Operation: "GET", ValueIsStatic: true,
			TargetUrl: "https://user:pass@example.com/orders?id=42#detail",
		},
	}
	result := planRootSpans(t, document)
	canary := "user:pass@example.com"
	var all string
	for _, span := range result.Items {
		all += span.GetName() + " "
		for _, attribute := range span.GetAttributes() {
			all += attribute.GetKey() + " " + attribute.GetValue().GetPath() + " "
		}
	}
	if strings.Contains(all, canary) {
		t.Fatalf("root span leaks canary: %s", all)
	}
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, canary) {
			t.Errorf("diagnostic leaks canary: %s", diagnostic.Message)
		}
	}
}

// TestMissingRouteDegradesAC6: an empty HTTP route falls back to the
// stable function identity with GEN_INCOMPLETE_TARGET and never uses the
// source path as a name.
func TestMissingRouteDegradesAC6(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints[0].HttpPath = ""
	result := planRootSpans(t, document)
	if len(result.Items) != 1 {
		t.Fatalf("span count = %d, want 1", len(result.Items))
	}
	span := result.Items[0]
	if span.GetName() != "checkout.HandleOrder" {
		t.Errorf("fallback name = %q, want the function identity", span.GetName())
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget && diagnostic.TargetID == "ep:orders" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GEN_INCOMPLETE_TARGET: %v", result.Diagnostics)
	}
	if strings.Contains(span.GetName(), ".go") || strings.Contains(span.GetName(), "/") {
		t.Errorf("fallback name leaks a source path: %q", span.GetName())
	}
}

// TestMissingGRPCFieldsFallback: missing gRPC service/method fall back to
// the function identity with a diagnostic.
func TestMissingGRPCFieldsFallback(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_GRPC_HANDLER)
	document.Endpoints[0].GrpcService = ""
	result := planRootSpans(t, document)
	if result.Items[0].GetName() != "checkout.HandleOrder" {
		t.Errorf("fallback name = %q", result.Items[0].GetName())
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget {
			found = true
		}
	}
	if !found {
		t.Errorf("missing fallback diagnostic: %v", result.Diagnostics)
	}
}

// TestUnknownEndpointKindSkipped: unknown kinds never produce a root.
func TestUnknownEndpointKindSkipped(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_GRPC_HANDLER)
	document.Endpoints[0].Kind = observabilityv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED
	result := planRootSpans(t, document)
	if len(result.Items) != 0 {
		t.Fatalf("unknown kind must not produce a root, got %d", len(result.Items))
	}
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
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

// TestRootSpanAttributeAllowlist: every emitted key is on the pinned
// trace allowlist and the unified vocabulary has no duplicate aliases.
func TestRootSpanAttributeAllowlist(t *testing.T) {
	attributes := naming.AttributePolicy{}
	for _, kind := range []observabilityv1.EndpointKind{
		observabilityv1.EndpointKind_HTTP_HANDLER,
		observabilityv1.EndpointKind_GRPC_HANDLER,
		observabilityv1.EndpointKind_CRON_JOB,
	} {
		result := planRootSpans(t, rootSpanDocument(kind))
		for _, span := range result.Items {
			for _, attribute := range span.GetAttributes() {
				if !attributes.TraceAttributeAllowed(attribute.GetKey()) {
					t.Errorf("span %s uses non-allowlisted attribute %q", span.GetId(), attribute.GetKey())
				}
			}
			// service.name appears once, service.version once: no aliases.
			serviceKeys := 0
			for _, attribute := range span.GetAttributes() {
				if strings.HasPrefix(attribute.GetKey(), "service.") {
					serviceKeys++
				}
			}
			if serviceKeys != 2 {
				t.Errorf("span %s has %d service.* attributes, want 2 (name, version)", span.GetId(), serviceKeys)
			}
		}
	}
}

// TestRootSpanStatusMapping: every root span carries the complete status
// mapping with no default fallthrough.
func TestRootSpanStatusMapping(t *testing.T) {
	for _, kind := range []observabilityv1.EndpointKind{
		observabilityv1.EndpointKind_HTTP_HANDLER,
		observabilityv1.EndpointKind_GRPC_HANDLER,
		observabilityv1.EndpointKind_CRON_JOB,
	} {
		result := planRootSpans(t, rootSpanDocument(kind))
		for _, span := range result.Items {
			status := span.GetStatus()
			if status == nil {
				t.Fatalf("span %s lacks a status policy", span.GetId())
			}
			errorSetting := observabilityv1.StatusSetting_STATUS_SETTING_ERROR
			unsetSetting := observabilityv1.StatusSetting_STATUS_SETTING_UNSET
			if status.GetOk() != unsetSetting || status.GetUnknown() != unsetSetting {
				t.Errorf("span %s ok/unknown must map to unset", span.GetId())
			}
			if status.GetError() != errorSetting || status.GetTimeout() != errorSetting || status.GetCancelled() != errorSetting {
				t.Errorf("span %s error/timeout/cancelled must map to error", span.GetId())
			}
		}
	}
}

// TestStrictViaPlanner: strict mode fails the plan on root span
// degradation warnings.
func TestStrictViaPlanner(t *testing.T) {
	document := rootSpanDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints[0].HttpPath = ""
	resolved, err := policy.Resolve(&policy.GenerationConfig{Strict: new(true)}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalTracing}
	plannerInstance := planner.New(planner.Options{Tracing: EndpointRootSpanPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err == nil {
		t.Fatal("strict mode must fail on GEN_INCOMPLETE_TARGET")
	}
	if plan != nil {
		t.Fatal("strict mode must not return a plan")
	}
}

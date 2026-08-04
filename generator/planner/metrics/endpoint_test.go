package metrics

import (
	"context"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// endpointDocument builds an IR with one function and one endpoint of the
// given kind.
func endpointDocument(kind observabilityv1.EndpointKind) *observabilityv1.ObservabilityDocument {
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

func planEndpointMetrics(t *testing.T, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) (*planner.MetricsResult, *planner.Index) {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	result, err := EndpointMetricsPlanner{}.PlanMetrics(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: resolved,
	})
	if err != nil {
		t.Fatalf("PlanMetrics failed: %v", err)
	}
	return result, index
}

// TestEndpointMetricsPlansComposite runs the full composite endpoint
// fixture through the real planner pipeline (AC1/AC6 covered end to end).
func TestEndpointMetricsPlansComposite(t *testing.T) {
	document := &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:http", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
			{Id: "fn:grpc", QualifiedName: "checkout.OrdersServer", PackagePath: "internal/orders"},
			{Id: "fn:cron", QualifiedName: "checkout.Cleanup", PackagePath: "internal/maintenance"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{
				Id: "ep:orders", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:http",
				HttpMethod: "POST", HttpPath: "/orders/{id}",
			},
			{
				Id: "ep:grpc", Kind: observabilityv1.EndpointKind_GRPC_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:grpc",
				GrpcService: "OrderService", GrpcMethod: "CreateOrder",
			},
			{
				Id: "ep:cron", Kind: observabilityv1.EndpointKind_CRON_JOB,
				Name: "Nightly Cleanup", FunctionId: "fn:cron", CronSchedule: "0 3 * * *",
			},
		},
	}
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{Metrics: EndpointMetricsPlanner{}})
	plan, report, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	// Defaults: gauge on, summary off -> 3 instruments per endpoint.
	if len(plan.Metrics) != 9 {
		t.Errorf("metric count = %d, want 9", len(plan.Metrics))
	}
	if report.Items.Metrics != 9 {
		t.Errorf("report metrics = %d, want 9", report.Items.Metrics)
	}
	if len(plan.Diagnostics) != 0 {
		t.Errorf("unexpected diagnostics: %v", plan.Diagnostics)
	}

	// Attribute vocabulary: exactly service/operation/status; no raw
	// values anywhere.
	for _, metric := range plan.Metrics {
		keys := attributeKeys(metric)
		hasStatus := metric.GetType() != observabilityv1.MetricType_METRIC_TYPE_GAUGE
		if hasStatus {
			if !containsKey(keys, "status") {
				t.Errorf("metric %s lacks status attribute", metric.GetId())
			}
		} else if containsKey(keys, "status") {
			t.Errorf("gauge %s carries status", metric.GetId())
		}
		if !containsKey(keys, "service") || !containsKey(keys, "operation") {
			t.Errorf("metric %s lacks service/operation attributes", metric.GetId())
		}
		if len(keys) != 3 && hasStatus || len(keys) != 2 && !hasStatus {
			t.Errorf("metric %s attribute count = %d", metric.GetId(), len(keys))
		}
		if metric.GetFunctionId() == "" {
			t.Errorf("metric %s lacks function reference", metric.GetId())
		}
	}
}

// TestHTTPDefaultsAC1: POST /orders/{id} yields counter, histogram and
// gauge, all referencing the endpoint, with the documented units.
func TestHTTPDefaultsAC1(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	if len(result.Items) != 3 {
		t.Fatalf("item count = %d, want 3", len(result.Items))
	}
	byType := map[observabilityv1.MetricType]*observabilityv1.MetricPlan{}
	for _, item := range result.Items {
		byType[item.GetType()] = item
	}
	counter := byType[observabilityv1.MetricType_METRIC_TYPE_COUNTER]
	histogram := byType[observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM]
	gauge := byType[observabilityv1.MetricType_METRIC_TYPE_GAUGE]
	if counter == nil || histogram == nil || gauge == nil {
		t.Fatalf("missing instrument types: %v", byType)
	}
	for _, item := range result.Items {
		if item.GetTarget().GetId() != "ep:orders" ||
			item.GetTarget().GetKind() != observabilityv1.TargetKind_TARGET_KIND_ENDPOINT {
			t.Errorf("item %s references wrong target", item.GetId())
		}
	}
	if counter.GetUnit() != "{request}" {
		t.Errorf("counter unit = %q", counter.GetUnit())
	}
	if histogram.GetUnit() != "s" {
		t.Errorf("histogram unit = %q", histogram.GetUnit())
	}
	if gauge.GetUnit() != "{operation}" {
		t.Errorf("gauge unit = %q", gauge.GetUnit())
	}
	if gauge.GetTrigger().GetPhase() != observabilityv1.TriggerPhase_TRIGGER_PHASE_STATE_CHANGE {
		t.Errorf("gauge trigger = %v", gauge.GetTrigger().GetPhase())
	}
	if gauge.GetValue().GetPath() != "runtime.operation.in_flight" {
		t.Errorf("gauge value path = %q", gauge.GetValue().GetPath())
	}
	// Names follow the P1-04 prefix rules (service/module/function/
	// operation) and end with the kind-specific purpose suffix.
	wantPrefix := "checkout_internal_orders_checkout_handleorder_post_"
	for _, item := range result.Items {
		if !strings.HasPrefix(item.GetName(), wantPrefix) {
			t.Errorf("metric %s name %q lacks the P1-04 prefix", item.GetId(), item.GetName())
		}
		if !strings.HasSuffix(item.GetName(), purposeSuffixFor(item.GetType())) {
			t.Errorf("metric %s name %q lacks purpose suffix", item.GetId(), item.GetName())
		}
	}
}

func purposeSuffixFor(metricType observabilityv1.MetricType) string {
	switch metricType {
	case observabilityv1.MetricType_METRIC_TYPE_COUNTER:
		return "http_requests_total"
	case observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM:
		return "http_request_duration_seconds"
	default:
		return "http_requests_in_flight"
	}
}

// TestGRPCAttributesAC2: operation is built from static service/method
// and status binds the finite runtime enum.
func TestGRPCAttributesAC2(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_GRPC_HANDLER)
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	if len(result.Items) != 3 {
		t.Fatalf("item count = %d, want 3", len(result.Items))
	}
	for _, item := range result.Items {
		operation := attributeValue(item, "operation")
		if operation == nil || operation.GetPath() != "plan.constant.orderservice_createorder" {
			t.Errorf("metric %s operation binding = %v", item.GetId(), operation)
		}
		status := attributeValue(item, "status")
		if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_GAUGE {
			if status != nil {
				t.Errorf("gauge carries status: %v", status)
			}
			continue
		}
		if status == nil || status.GetPath() != "runtime.operation.status" ||
			status.GetType() != observabilityv1.ValueType_VALUE_TYPE_STATUS {
			t.Errorf("metric %s status binding = %v", item.GetId(), status)
		}
	}
}

// TestCronNoHighCardinalityAC3: cron metrics carry no schedule, execution
// time or payload attributes.
func TestCronNoHighCardinalityAC3(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_CRON_JOB)
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	for _, item := range result.Items {
		for _, attribute := range item.GetAttributes() {
			if strings.Contains(attribute.GetKey(), "schedule") ||
				strings.Contains(attribute.GetKey(), "time") ||
				strings.Contains(attribute.GetKey(), "payload") {
				t.Errorf("cron metric %s carries high-cardinality attribute %q", item.GetId(), attribute.GetKey())
			}
		}
	}
}

// TestSummaryPolicyAC4: summary is absent by default and exactly one
// duration summary appears when enabled, using the configured quantiles.
func TestSummaryPolicyAC4(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	defaults, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *defaults)
	for _, item := range result.Items {
		if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_SUMMARY {
			t.Fatal("summary must be absent by default")
		}
	}

	enabled, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			Summaries: &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.9}},
		},
	}, nil)
	result, _ = planEndpointMetrics(t, document, *enabled)
	count := 0
	for _, item := range result.Items {
		if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_SUMMARY {
			count++
			if item.GetValue().GetPath() != "runtime.operation.duration_seconds" {
				t.Errorf("summary value source = %q", item.GetValue().GetPath())
			}
		}
	}
	if count != 1 {
		t.Errorf("summary count = %d, want exactly 1", count)
	}
}

// TestGaugePolicyMatrix: the in-flight gauge disappears when the policy
// disables it.
func TestGaugePolicyMatrix(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	disabled, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{IncludeInFlightGauges: new(false)},
	}, nil)
	result, _ := planEndpointMetrics(t, document, *disabled)
	if len(result.Items) != 2 {
		t.Fatalf("item count = %d, want 2 without gauge", len(result.Items))
	}
	for _, item := range result.Items {
		if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_GAUGE {
			t.Fatal("gauge must be absent when disabled")
		}
	}
}

// TestCustomBucketsAndQuantilesFlowIntoEstimates: custom buckets change
// the series estimate but never the plan content (buckets live in the
// policy, rendered later).
func TestCustomBucketsAndQuantilesFlowIntoEstimates(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	custom, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			HistogramBucketsSeconds: []float64{0.1, 1},
			Summaries:               &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5}},
		},
	}, nil)
	result, _ := planEndpointMetrics(t, document, *custom)
	// 3 instruments with summary; no error because the budget still fits.
	if len(result.Items) != 4 {
		t.Fatalf("item count = %d, want 4", len(result.Items))
	}
}

// TestUnknownEndpointKindSkipped: an unknown kind is skipped with
// GEN_UNSUPPORTED_ENTITY, never guessed.
func TestUnknownEndpointKindSkipped(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_GRPC_HANDLER)
	document.Endpoints[0].Kind = observabilityv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED
	document.Endpoints[0].Name = "Mystery"
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	if len(result.Items) != 0 {
		t.Fatalf("unknown kind must produce no items, got %d", len(result.Items))
	}
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != policy.CodeUnsupportedEntity {
		t.Errorf("diagnostics = %v", result.Diagnostics)
	}
}

// TestMissingHTTPMethodDegradesAC5: missing method/route produces a
// generic metric and GEN_INCOMPLETE_TARGET without raw paths.
func TestMissingHTTPMethodDegradesAC5(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints[0].HttpMethod = ""
	document.Endpoints[0].HttpPath = ""
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	if len(result.Items) == 0 {
		t.Fatal("generic metrics must still be generated")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget && diagnostic.TargetID == "ep:orders" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing GEN_INCOMPLETE_TARGET diagnostic: %v", result.Diagnostics)
	}
	for _, item := range result.Items {
		if strings.Contains(item.GetName(), "/") {
			t.Errorf("metric name leaks a path: %q", item.GetName())
		}
	}
}

// TestGRPCMissingFieldsFallback: missing service/method falls back to the
// function identity with a diagnostic.
func TestGRPCMissingFieldsFallback(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_GRPC_HANDLER)
	document.Endpoints[0].GrpcService = ""
	document.Endpoints[0].GrpcMethod = ""
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	if len(result.Items) == 0 {
		t.Fatal("fallback metrics must still be generated")
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
	for _, item := range result.Items {
		operation := attributeValue(item, "operation")
		if operation != nil && !strings.Contains(operation.GetPath(), "plan.constant.") {
			t.Errorf("operation binding has no constant path: %v", operation)
		}
	}
}

// TestCardinalityNoRawValues: raw URL/query values never enter names,
// descriptions, attributes or diagnostics.
func TestCardinalityNoRawValues(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints[0].HttpPath = "https://user:pass@example.com/orders?id=42#detail"
	document.Endpoints[0].HttpMethod = "POST"
	resolved, _ := policy.Resolve(nil, nil)
	result, _ := planEndpointMetrics(t, document, *resolved)
	canary := "user:pass@example.com"
	var all string
	for _, item := range result.Items {
		all += item.GetName() + " " + item.GetDescription() + " "
		for _, attribute := range item.GetAttributes() {
			all += attribute.GetKey() + " " + attribute.GetValue().GetPath() + " "
		}
	}
	if strings.Contains(all, canary) {
		t.Fatalf("plan leaks canary: %s", all)
	}
	for _, diagnostic := range result.Diagnostics {
		if strings.Contains(diagnostic.Message, canary) {
			t.Errorf("diagnostic leaks canary: %s", diagnostic.Message)
		}
	}
}

// TestBudgetExceededFailsWholeSignal: exceeding max_estimated_series
// fails the whole signal with GEN_CARDINALITY_LIMIT_EXCEEDED.
func TestBudgetExceededFailsWholeSignal(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	tight, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{MaxEstimatedSeries: new(int64(1))},
	}, nil)
	_, err = EndpointMetricsPlanner{}.PlanMetrics(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: *tight,
	})
	if err == nil {
		t.Fatal("budget overflow must fail")
	}
	if !strings.Contains(err.Error(), policy.CodeCardinalityLimitExceeded) {
		t.Errorf("error %q lacks the cardinality code", err.Error())
	}
}

// TestStrictViaPlannerAC6: strict mode fails the whole plan on endpoint
// degradation warnings.
func TestStrictViaPlannerAC6(t *testing.T) {
	document := endpointDocument(observabilityv1.EndpointKind_HTTP_HANDLER)
	document.Endpoints[0].HttpMethod = ""
	document.Endpoints[0].HttpPath = ""
	resolved, _ := policy.Resolve(&policy.GenerationConfig{Strict: new(true)}, nil)
	plannerInstance := planner.New(planner.Options{Metrics: EndpointMetricsPlanner{}})
	plan, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err == nil {
		t.Fatal("strict mode must fail on GEN_INCOMPLETE_TARGET")
	}
	if plan != nil {
		t.Fatal("strict mode must not return a plan")
	}
}

func attributeKeys(metric *observabilityv1.MetricPlan) []string {
	keys := make([]string, 0, len(metric.GetAttributes()))
	for _, attribute := range metric.GetAttributes() {
		keys = append(keys, attribute.GetKey())
	}
	return keys
}

func containsKey(keys []string, wanted string) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}

func attributeValue(metric *observabilityv1.MetricPlan, key string) *observabilityv1.ValueBinding {
	for _, attribute := range metric.GetAttributes() {
		if attribute.GetKey() == key {
			return attribute.GetValue()
		}
	}
	return nil
}

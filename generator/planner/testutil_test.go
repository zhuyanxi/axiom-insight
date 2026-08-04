package planner

import (
	"context"
	"sync/atomic"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// testDocument builds a valid composite IR: one HTTP endpoint, one SQL
// dependency, a resolved internal call edge and their functions.
func testDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{
				Id: "fn:handler", QualifiedName: "checkout.HandleOrder",
				InputEndpointIds:  []string{"ep:orders"},
				DependencyIds:     []string{"dep:orders-sql"},
				CalleeFunctionIds: []string{"fn:validator"},
			},
			{Id: "fn:validator", QualifiedName: "checkout.Validate"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{
				Id: "ep:orders", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
				Name: "CreateOrder", FunctionId: "fn:handler",
				HttpMethod: "POST", HttpPath: "/orders/{id}",
			},
		},
		Dependencies: []*observabilityv1.Dependency{
			{
				Id: "dep:orders-sql", Kind: observabilityv1.DependencyKind_SQL,
				Name: "OrdersStore", FunctionId: "fn:handler", Operation: "exec",
			},
		},
		CallEdges: []*observabilityv1.CallEdge{
			{
				Id: "edge:handler-validator", CallerFunctionId: "fn:handler",
				CalleeFunctionId: "fn:validator", Resolution: observabilityv1.CallResolution_RESOLVED,
			},
		},
	}
}

// stubMetricsPlanner emits one counter per endpoint and dependency,
// records invocations and can inject diagnostics.
type stubMetricsPlanner struct {
	calls      atomic.Int64
	skipped    int
	diagnostics []naming.Diagnostic
}

func (stub *stubMetricsPlanner) PlanMetrics(ctx context.Context, input *SignalInput) (*MetricsResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stub.calls.Add(1)
	var items []*observabilityv1.MetricPlan
	for _, endpoint := range input.Document.Endpoints {
		items = append(items, stubCounter(endpoint.GetId(), observabilityv1.TargetKind_TARGET_KIND_ENDPOINT))
	}
	for _, dependency := range input.Document.Dependencies {
		items = append(items, stubCounter(dependency.GetId(), observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY))
	}
	return &MetricsResult{Items: items, Diagnostics: append([]naming.Diagnostic(nil), stub.diagnostics...), Skipped: stub.skipped}, nil
}

// stubTracingPlanner emits one span per endpoint and dependency.
type stubTracingPlanner struct {
	calls       atomic.Int64
	skipped     int
	diagnostics []naming.Diagnostic
}

func (stub *stubTracingPlanner) PlanTracing(ctx context.Context, input *SignalInput) (*TracingResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stub.calls.Add(1)
	var items []*observabilityv1.SpanPlan
	for _, endpoint := range input.Document.Endpoints {
		items = append(items, stubSpan(endpoint.GetId(), observabilityv1.TargetKind_TARGET_KIND_ENDPOINT))
	}
	for _, dependency := range input.Document.Dependencies {
		items = append(items, stubSpan(dependency.GetId(), observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY))
	}
	return &TracingResult{Items: items, Diagnostics: append([]naming.Diagnostic(nil), stub.diagnostics...), Skipped: stub.skipped}, nil
}

// stubLoggingPlanner emits one log event per endpoint.
type stubLoggingPlanner struct {
	calls       atomic.Int64
	skipped     int
	diagnostics []naming.Diagnostic
}

func (stub *stubLoggingPlanner) PlanLogging(ctx context.Context, input *SignalInput) (*LoggingResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stub.calls.Add(1)
	var items []*observabilityv1.LogPlan
	for _, endpoint := range input.Document.Endpoints {
		items = append(items, &observabilityv1.LogPlan{
			Id:        StableID(SignalLogging, endpoint.GetId(), PurposeEnd),
			EventName: "http.request.completed",
			Severity:  observabilityv1.LogSeverity_LOG_SEVERITY_INFO,
			Target:    &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId()},
			Trigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
		})
	}
	return &LoggingResult{Items: items, Diagnostics: append([]naming.Diagnostic(nil), stub.diagnostics...), Skipped: stub.skipped}, nil
}

func stubCounter(targetID string, kind observabilityv1.TargetKind) *observabilityv1.MetricPlan {
	return &observabilityv1.MetricPlan{
		Id:   StableID(SignalMetrics, targetID, PurposeCount),
		Name: "requests_total",
		Type: observabilityv1.MetricType_METRIC_TYPE_COUNTER,
		Unit: "{request}",
		Target: &observabilityv1.TargetRef{Kind: kind, Id: targetID},
		Trigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
		Value: &observabilityv1.ValueBinding{
			Path: "plan.constant.one", Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_INT64, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
		Attributes: []*observabilityv1.AttributeBinding{
			{Key: "status", Value: statusBinding()},
		},
	}
}

func stubSpan(targetID string, kind observabilityv1.TargetKind) *observabilityv1.SpanPlan {
	return &observabilityv1.SpanPlan{
		Id:   StableID(SignalTracing, targetID, PurposeChild),
		Name: "operation",
		Kind: observabilityv1.SpanKind_SPAN_KIND_CLIENT,
		Target: &observabilityv1.TargetRef{Kind: kind, Id: targetID},
		StartTrigger: &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_START},
		EndTrigger:   &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
		Parent:       &observabilityv1.ParentStrategy{Mode: observabilityv1.ParentStrategyMode_PARENT_STRATEGY_MODE_CURRENT_CONTEXT},
		Status:       fullStatusPolicy(),
	}
}

func fullStatusPolicy() *observabilityv1.StatusPolicy {
	unset := observabilityv1.StatusSetting_STATUS_SETTING_UNSET
	return &observabilityv1.StatusPolicy{Ok: unset, Error: unset, Timeout: unset, Cancelled: unset, Unknown: unset}
}

func statusBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.operation.status", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_STATUS, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

// defaultPolicy enables all three signals with defaults.
func defaultPolicy() policy.Policy {
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		panic(err)
	}
	return *resolved
}

package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func newTestPlanner() (*Planner, *stubMetricsPlanner, *stubTracingPlanner, *stubLoggingPlanner) {
	metrics := &stubMetricsPlanner{}
	tracing := &stubTracingPlanner{}
	logging := &stubLoggingPlanner{}
	return New(Options{Metrics: metrics, Tracing: tracing, Logging: logging}), metrics, tracing, logging
}

// TestPlanValidIRProducesCompletePlan is AC1: a valid IR yields a non-nil
// plan with no fatal error, and all three sub-planners receive entities.
func TestPlanValidIRProducesCompletePlan(t *testing.T) {
	planner, metrics, tracing, logging := newTestPlanner()
	plan, report, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if plan.SchemaVersion != observabilityv1.GenerationPlanSchemaVersion {
		t.Errorf("schema version = %q", plan.SchemaVersion)
	}
	if plan.SourceIrSchemaVersion != "v1" {
		t.Errorf("source IR schema version = %q", plan.SourceIrSchemaVersion)
	}
	if plan.ServiceName != "checkout" {
		t.Errorf("service name = %q", plan.ServiceName)
	}
	if len(plan.Metrics) != 2 {
		t.Errorf("metric count = %d, want 2", len(plan.Metrics))
	}
	if len(plan.Spans) != 2 {
		t.Errorf("span count = %d, want 2", len(plan.Spans))
	}
	if len(plan.Logs) != 1 {
		t.Errorf("log count = %d, want 1", len(plan.Logs))
	}
	if metrics.calls.Load() != 1 || tracing.calls.Load() != 1 || logging.calls.Load() != 1 {
		t.Errorf("sub-planner calls metrics=%d tracing=%d logging=%d, want 1 each",
			metrics.calls.Load(), tracing.calls.Load(), logging.calls.Load())
	}
	if report.Input.Functions != 2 || report.Input.Endpoints != 1 ||
		report.Input.Dependencies != 1 || report.Input.CallEdges != 1 {
		t.Errorf("input census wrong: %+v", report.Input)
	}
	if report.Items.Metrics != 2 || report.Items.Spans != 2 || report.Items.Logs != 1 {
		t.Errorf("item counts wrong: %+v", report.Items)
	}
}

// TestPlanItemsAreSorted: plan items, attributes and events are in stable
// order regardless of sub-planner insertion order.
func TestPlanItemsAreSorted(t *testing.T) {
	planner, _, _, _ := newTestPlanner()
	plan, _, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	for index := 1; index < len(plan.Metrics); index++ {
		if plan.Metrics[index-1].GetId() >= plan.Metrics[index].GetId() {
			t.Errorf("metrics not sorted: %q before %q", plan.Metrics[index-1].GetId(), plan.Metrics[index].GetId())
		}
	}
	for index := 1; index < len(plan.Spans); index++ {
		if plan.Spans[index-1].GetId() >= plan.Spans[index].GetId() {
			t.Errorf("spans not sorted: %q before %q", plan.Spans[index-1].GetId(), plan.Spans[index].GetId())
		}
	}
	for index := 1; index < len(plan.Logs); index++ {
		if plan.Logs[index-1].GetId() >= plan.Logs[index].GetId() {
			t.Errorf("logs not sorted: %q before %q", plan.Logs[index-1].GetId(), plan.Logs[index].GetId())
		}
	}
}

// TestPlanDanglingReferenceBlocksPlan is AC2: a dependency referencing a
// missing function fails with GEN_DANGLING_REFERENCE, the entity ID and
// the field name, and no partial plan is returned.
func TestPlanDanglingReferenceBlocksPlan(t *testing.T) {
	document := testDocument()
	document.Dependencies[0].FunctionId = "fn:missing"
	planner, metrics, _, _ := newTestPlanner()
	plan, _, err := planner.Plan(context.Background(), document, defaultPolicy())
	if plan != nil {
		t.Fatal("plan must be nil on dangling reference")
	}
	if err == nil {
		t.Fatal("Plan must fail on dangling reference")
	}
	var invalid *InvalidIRError
	if !errors.As(err, &invalid) {
		t.Fatalf("error type = %T, want *InvalidIRError", err)
	}
	message := err.Error()
	if !strings.Contains(message, "GEN_DANGLING_REFERENCE") {
		t.Errorf("error %q lacks GEN_DANGLING_REFERENCE", message)
	}
	if !strings.Contains(message, "dep:orders-sql") {
		t.Errorf("error %q lacks the entity ID", message)
	}
	if !strings.Contains(message, "function_id") {
		t.Errorf("error %q lacks the field name", message)
	}
	if metrics.calls.Load() != 0 {
		t.Error("sub-planners must not run after a fatal validation error")
	}
}

// TestPlanDynamicTargetDegradesNonStrict is AC3: a dynamic target with
// complete references keeps generic definitions, omits target attributes
// and reports GEN_INCOMPLETE_TARGET in non-strict mode.
func TestPlanDynamicTargetDegradesNonStrict(t *testing.T) {
	metrics := &stubMetricsPlanner{diagnostics: []naming.Diagnostic{{
		Code: policy.CodeIncompleteTarget, Signal: SignalMetrics,
		TargetID: "dep:http-client", Field: "target",
		Message: "dynamic target; target attributes omitted",
	}}}
	planner := New(Options{Metrics: metrics, Tracing: &stubTracingPlanner{}, Logging: &stubLoggingPlanner{}})
	plan, report, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err != nil {
		t.Fatalf("non-strict plan must succeed with warnings: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if len(plan.Diagnostics) != 1 {
		t.Fatalf("plan diagnostics = %d, want 1", len(plan.Diagnostics))
	}
	diagnostic := plan.Diagnostics[0]
	if diagnostic.GetCode() != policy.CodeIncompleteTarget {
		t.Errorf("diagnostic code = %q", diagnostic.GetCode())
	}
	if diagnostic.GetSeverity() != observabilityv1.PlanSeverity_PLAN_SEVERITY_WARNING {
		t.Errorf("diagnostic severity = %v, want warning", diagnostic.GetSeverity())
	}
	found := false
	for _, count := range report.GeneratorDiagnostics {
		if count.Code == policy.CodeIncompleteTarget && count.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("report misses the warning count: %v", report.GeneratorDiagnostics)
	}
}

// TestPlanStrictPromotesWarnings is AC4: the same input fails in strict
// mode without a committable plan.
func TestPlanStrictPromotesWarnings(t *testing.T) {
	metrics := &stubMetricsPlanner{diagnostics: []naming.Diagnostic{{
		Code: policy.CodeIncompleteTarget, Signal: SignalMetrics,
		TargetID: "dep:http-client", Field: "target",
		Message: "dynamic target; target attributes omitted",
	}}}
	planner := New(Options{Metrics: metrics, Tracing: &stubTracingPlanner{}, Logging: &stubLoggingPlanner{}})
	strict := defaultPolicy()
	strict.Strict = true
	plan, _, err := planner.Plan(context.Background(), testDocument(), strict)
	if err == nil {
		t.Fatal("strict mode must fail on warnings")
	}
	if plan != nil {
		t.Fatal("strict mode must not return a plan")
	}
}

// TestPlanSourceDiagnosticsStaySourceLevel: Phase 0 source diagnostics
// are counted in the report but keep their original level; strict mode
// does not promote them.
func TestPlanSourceDiagnosticsStaySourceLevel(t *testing.T) {
	document := testDocument()
	document.Diagnostics = []*observabilityv1.Diagnostic{
		{
			Severity: observabilityv1.DiagnosticSeverity_WARNING,
			Code:     "ANALYZER_UNRESOLVED_CALL",
			Message:  "call target could not be resolved statically",
		},
	}
	planner, _, _, _ := newTestPlanner()
	strict := defaultPolicy()
	strict.Strict = true
	plan, report, err := planner.Plan(context.Background(), document, strict)
	if err != nil {
		t.Fatalf("source warnings must not fail strict planning: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if len(plan.Diagnostics) != 0 {
		t.Errorf("source diagnostics must not enter the plan: %v", plan.Diagnostics)
	}
	found := false
	for _, count := range report.SourceDiagnostics {
		if count.Code == "ANALYZER_UNRESOLVED_CALL" && count.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Errorf("report misses source diagnostics: %v", report.SourceDiagnostics)
	}
}

// TestPlanCancellationAC6: a cancelled context returns context.Canceled,
// runs no sub-planner and produces no plan.
func TestPlanCancellationAC6(t *testing.T) {
	planner, metrics, tracing, logging := newTestPlanner()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, _, err := planner.Plan(ctx, testDocument(), defaultPolicy())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if plan != nil {
		t.Fatal("plan must be nil after cancellation")
	}
	if metrics.calls.Load() != 0 || tracing.calls.Load() != 0 || logging.calls.Load() != 0 {
		t.Error("sub-planners must not run after cancellation")
	}
}

// TestPlanInputNotModified is AC7: the input document bytes are identical
// before and after planning.
func TestPlanInputNotModified(t *testing.T) {
	document := testDocument()
	before, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	planner, _, _, _ := newTestPlanner()
	if _, _, err := planner.Plan(context.Background(), document, defaultPolicy()); err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	after, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("planning modified the input document")
	}
}

// TestPlanDisabledSignalsSkipped: a policy that disables a signal never
// invokes its sub-planner.
func TestPlanDisabledSignalsSkipped(t *testing.T) {
	planner, metrics, tracing, logging := newTestPlanner()
	disabled := defaultPolicy()
	disabled.Signals = []string{"metrics"}
	plan, _, err := planner.Plan(context.Background(), testDocument(), disabled)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	if metrics.calls.Load() != 1 {
		t.Error("metrics planner must run")
	}
	if tracing.calls.Load() != 0 || logging.calls.Load() != 0 {
		t.Error("disabled signal planners must not run")
	}
	if len(plan.Spans) != 0 || len(plan.Logs) != 0 {
		t.Error("disabled signals must produce no items")
	}
}

// TestPlanUnregisteredEnabledSignalFails: enabling a signal without a
// registered sub-planner is a wiring error.
func TestPlanUnregisteredEnabledSignalFails(t *testing.T) {
	planner := New(Options{})
	plan, _, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err == nil {
		t.Fatal("unregistered metrics planner must fail")
	}
	if !strings.Contains(err.Error(), SignalMetrics) {
		t.Errorf("error %q lacks the signal context", err.Error())
	}
	if plan != nil {
		t.Fatal("plan must be nil")
	}
}

// TestPlanDuplicateIDsAcrossSignalsRejected: a sub-planner emitting the
// same plan ID twice fails the plan.
func TestPlanDuplicateIDsAcrossSignalsRejected(t *testing.T) {
	metrics := &duplicateIDMetricsPlanner{}
	planner := New(Options{Metrics: metrics, Tracing: &stubTracingPlanner{}, Logging: &stubLoggingPlanner{}})
	plan, _, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err == nil {
		t.Fatal("duplicate plan IDs must fail")
	}
	if plan != nil {
		t.Fatal("plan must be nil on duplicate IDs")
	}
	if !strings.Contains(err.Error(), "duplicate plan ID") {
		t.Errorf("error %q lacks duplicate ID message", err.Error())
	}
}

// TestPlanTargetKindMismatchRejected: a plan item targeting an endpoint
// with a dependency kind fails.
func TestPlanTargetKindMismatchRejected(t *testing.T) {
	metrics := &wrongKindMetricsPlanner{}
	planner := New(Options{Metrics: metrics, Tracing: &stubTracingPlanner{}, Logging: &stubLoggingPlanner{}})
	plan, _, err := planner.Plan(context.Background(), testDocument(), defaultPolicy())
	if err == nil {
		t.Fatal("target kind mismatch must fail")
	}
	if plan != nil {
		t.Fatal("plan must be nil on target kind mismatch")
	}
}

// duplicateIDMetricsPlanner emits the same plan ID for every entity.
type duplicateIDMetricsPlanner struct{}

func (*duplicateIDMetricsPlanner) PlanMetrics(_ context.Context, _ *SignalInput) (*MetricsResult, error) {
	items := []*observabilityv1.MetricPlan{
		stubCounter("ep:dupe", observabilityv1.TargetKind_TARGET_KIND_ENDPOINT),
		stubCounter("ep:dupe", observabilityv1.TargetKind_TARGET_KIND_ENDPOINT),
	}
	return &MetricsResult{Items: items}, nil
}

// wrongKindMetricsPlanner targets an endpoint ID with a dependency kind.
type wrongKindMetricsPlanner struct{}

func (*wrongKindMetricsPlanner) PlanMetrics(_ context.Context, input *SignalInput) (*MetricsResult, error) {
	var items []*observabilityv1.MetricPlan
	for _, endpoint := range input.Document.Endpoints {
		items = append(items, stubCounter(endpoint.GetId(), observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY))
	}
	return &MetricsResult{Items: items}, nil
}

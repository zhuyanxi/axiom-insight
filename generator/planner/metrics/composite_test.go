package metrics

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// compositeAllTypesDocument combines three endpoint kinds with all six
// dependency kinds in one IR.
func compositeAllTypesDocument() *observabilityv1.ObservabilityDocument {
	document := sixKindDocument()
	document.Endpoints = []*observabilityv1.Endpoint{
		{
			Id: "ep:orders", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
			Name: "CreateOrder", FunctionId: "fn:service",
			HttpMethod: "POST", HttpPath: "/orders/{id}",
		},
		{
			Id: "ep:grpc", Kind: observabilityv1.EndpointKind_GRPC_HANDLER,
			Name: "CreateOrder", FunctionId: "fn:service",
			GrpcService: "OrderService", GrpcMethod: "CreateOrder",
		},
		{
			Id: "ep:cron", Kind: observabilityv1.EndpointKind_CRON_JOB,
			Name: "Nightly Cleanup", FunctionId: "fn:service", CronSchedule: "0 3 * * *",
		},
	}
	return document
}

// TestAllMetricTypesCompositeAC6: with gauge and summary enabled, the
// merged endpoint + dependency plan expresses all four metric types and
// passes the P1-01 plan validator (the planner runs it before returning).
func TestAllMetricTypesCompositeAC6(t *testing.T) {
	document := compositeAllTypesDocument()
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			Summaries: &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.9, 0.99}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{
		Metrics: CompositeMetricsPlanner{Endpoint: EndpointMetricsPlanner{}, Dependency: DependencyMetricsPlanner{}},
	})
	plan, report, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if plan == nil {
		t.Fatal("plan is nil")
	}
	// 3 endpoints x 4 + 6 dependencies x 4 = 36 instruments.
	if len(plan.Metrics) != 36 {
		t.Errorf("metric count = %d, want 36", len(plan.Metrics))
	}
	if report.Items.Metrics != 36 {
		t.Errorf("report metrics = %d, want 36", report.Items.Metrics)
	}
	types := map[observabilityv1.MetricType]bool{}
	for _, metric := range plan.Metrics {
		types[metric.GetType()] = true
	}
	for _, metricType := range []observabilityv1.MetricType{
		observabilityv1.MetricType_METRIC_TYPE_COUNTER,
		observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM,
		observabilityv1.MetricType_METRIC_TYPE_GAUGE,
		observabilityv1.MetricType_METRIC_TYPE_SUMMARY,
	} {
		if !types[metricType] {
			t.Errorf("metric type %v missing from the composite plan", metricType)
		}
	}
	// All names must be unique: the plan validator would reject
	// duplicates, but assert explicitly for the merged signal.
	names := map[string]bool{}
	for _, metric := range plan.Metrics {
		if names[metric.GetName()] {
			t.Errorf("duplicate metric name in merged plan: %q", metric.GetName())
		}
		names[metric.GetName()] = true
	}
}

// TestCombinedBudgetAcrossPlanners: endpoint and dependency metrics share
// one budget; the union cannot exceed max_estimated_series even when each
// planner alone fits.
func TestCombinedBudgetAcrossPlanners(t *testing.T) {
	document := compositeAllTypesDocument()
	// Endpoint alone: 3 endpoints x ~24 series = 72; dependency alone:
	// 6 x ~24 = 144. A 150 limit passes each planner but the union must
	// fail.
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{MaxEstimatedSeries: new(int64(150))},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{
		Metrics: CompositeMetricsPlanner{Endpoint: EndpointMetricsPlanner{}, Dependency: DependencyMetricsPlanner{}},
	})
	plan, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err == nil {
		t.Fatal("combined series budget must fail")
	}
	if plan != nil {
		t.Fatal("plan must be nil on budget overflow")
	}
	if !strings.Contains(err.Error(), policy.CodeCardinalityLimitExceeded) {
		t.Errorf("error %q lacks the cardinality code", err.Error())
	}
}

// TestCanaryBytesScan: serialized plan bytes and diagnostics contain none
// of the canary values, even with dynamic targets.
func TestCanaryBytesScan(t *testing.T) {
	document := compositeAllTypesDocument()
	for _, dependency := range document.Dependencies {
		dependency.Value = "canary-value-9f8e7d6c"
		dependency.TargetUrl = "https://user:pass@canary-9f8e7d6c.example/orders?id=42"
		dependency.Resource = "canary-resource-9f8e7d6c"
		dependency.ValueIsStatic = false
	}
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{
		Metrics: CompositeMetricsPlanner{Endpoint: EndpointMetricsPlanner{}, Dependency: DependencyMetricsPlanner{}},
	})
	plan, _, err := plannerInstance.Plan(context.Background(), document, *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	bytes, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	for _, canary := range []string{"canary-value-9f8e7d6c", "canary-9f8e7d6c", "canary-resource-9f8e7d6c", "user:pass"} {
		if strings.Contains(string(bytes), canary) {
			t.Errorf("serialized plan leaks canary %q", canary)
		}
	}
	for _, diagnostic := range plan.GetDiagnostics() {
		if strings.Contains(diagnostic.GetMessage(), "9f8e7d6c") {
			t.Errorf("diagnostic leaks canary: %s", diagnostic.GetMessage())
		}
	}
}

// TestGoldenEndpointAndDependencyMetrics pins the merged plan bytes for
// the full endpoint + dependency fixture (P1-07 golden requirement).
func TestGoldenEndpointAndDependencyMetrics(t *testing.T) {
	resolved, err := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			Summaries: &policy.SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.9, 0.99}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	resolved.Signals = []string{planner.SignalMetrics}
	plannerInstance := planner.New(planner.Options{
		Metrics: CompositeMetricsPlanner{Endpoint: EndpointMetricsPlanner{}, Dependency: DependencyMetricsPlanner{}},
	})
	plan, _, err := plannerInstance.Plan(context.Background(), compositeAllTypesDocument(), *resolved)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	contents, err := protojson.MarshalOptions{UseProtoNames: true, Indent: "  "}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "golden", "endpoint_dependency_metrics.json")
	if os.Getenv(updateGoldenEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, contents, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden file %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s (set %s=1 to regenerate): %v", goldenPath, updateGoldenEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("merged metrics differ from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
	}
}

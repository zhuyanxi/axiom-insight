package pipeline

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/planner/logging"
	"github.com/zhuyanxi/axiom-insight/generator/planner/metrics"
	"github.com/zhuyanxi/axiom-insight/generator/planner/tracing"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// TestP213SensitiveCanaryFullChain checks IR -> GenerationPlan -> Catalog ->
// Dashboard render. Raw dependency values must not reach any dashboard
// artifact, diagnostic or error string.
func TestP213SensitiveCanaryFullChain(t *testing.T) {
	document := p213SensitiveDocument()
	plan, err := p213PlanDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	includeClients := false
	resolved, err := dashboard.Resolve(&dashboard.DashboardConfig{IncludeClientDependencies: &includeClients}, nil)
	if err != nil {
		t.Fatalf("dashboard policy: %v", err)
	}
	catalog, err := dashboard.BuildCatalog(document, plan, *resolved)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	dashboardPlan, err := Build(catalog, *resolved)
	if err != nil {
		t.Fatalf("dashboard plan: %v", err)
	}
	result, err := Render(dashboardPlan)
	if err != nil {
		t.Fatalf("dashboard render: %v", err)
	}
	contents := string(result.Bytes)
	for _, diagnostic := range dashboardPlan.Diagnostics() {
		contents += " " + diagnostic.Message
	}
	for _, canary := range p213Canaries {
		if strings.Contains(contents, canary) {
			t.Fatalf("canary %q leaked through dashboard pipeline", canary)
		}
	}
}

var p213Canaries = []string{
	"canary-password-hunter2", "canary-redis-key-7f3a", "canary-user",
	"canary-pass", "canary.example", "canary-fragment", "canary-topic-9f8e7d6c",
	"canary-payload-b1a2c3", "canary-email-4d5e6f@example.com",
}

func p213SensitiveDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "sensitive"},
		Functions:     []*observabilityv1.Function{{Id: "fn:f", QualifiedName: "sensitive.F"}},
		Endpoints: []*observabilityv1.Endpoint{{
			Id: "ep:http", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
			Name: "process", FunctionId: "fn:f", HttpMethod: "POST", HttpPath: "/process",
		}},
		Dependencies: []*observabilityv1.Dependency{
			{Id: "dep:sql", Kind: observabilityv1.DependencyKind_SQL, Name: "Store", FunctionId: "fn:f", Operation: "query", Value: "SELECT * FROM users WHERE password = 'canary-password-hunter2'", ValueIsStatic: true},
			{Id: "dep:redis", Kind: observabilityv1.DependencyKind_REDIS, Name: "Cache", FunctionId: "fn:f", Operation: "get", Value: "user:42:session:canary-redis-key-7f3a", ValueIsStatic: true},
			{Id: "dep:http", Kind: observabilityv1.DependencyKind_HTTP_CLIENT, Name: "External", FunctionId: "fn:f", Operation: "GET", TargetUrl: "https://canary-user:canary-pass@canary.example/orders?id=42#canary-fragment", ValueIsStatic: true},
			{Id: "dep:producer", Kind: observabilityv1.DependencyKind_KAFKA_PRODUCER, Name: "Events", FunctionId: "fn:f", Operation: "produce", Resource: "canary-topic-9f8e7d6c", Value: "{\"event\":\"canary-payload-b1a2c3\"}", ValueIsStatic: true},
		},
		Diagnostics: []*observabilityv1.Diagnostic{{
			Severity: observabilityv1.DiagnosticSeverity_WARNING,
			Code:     "ANALYZER_SENSITIVE_VALUE", Message: "sensitive value canary-email-4d5e6f@example.com detected",
		}},
	}
}

func p213PlanDocument(t *testing.T, document *observabilityv1.ObservabilityDocument) (*observabilityv1.GenerationPlan, error) {
	t.Helper()
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	pipeline := planner.New(planner.Options{
		Metrics: metrics.CompositeMetricsPlanner{Endpoint: metrics.EndpointMetricsPlanner{}, Dependency: metrics.DependencyMetricsPlanner{}},
		Tracing: tracing.CompositeTracingPlanner{Root: tracing.EndpointRootSpanPlanner{}, Dependency: tracing.DependencyChildSpanPlanner{}, Internal: tracing.InternalCallSpanPlanner{}},
		Logging: logging.LoggingPlanner{},
	})
	plan, _, err := pipeline.Plan(t.Context(), document, *resolved)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	return plan, nil
}

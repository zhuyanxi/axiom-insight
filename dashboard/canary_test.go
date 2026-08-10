package dashboard

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/planner/logging"
	"github.com/zhuyanxi/axiom-insight/generator/planner/metrics"
	"github.com/zhuyanxi/axiom-insight/generator/planner/tracing"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// canaries are the sensitive fixture values that must never survive into
// the catalog, its diagnostics or its errors.
var canaries = []string{
	"canary-password-hunter2",
	"canary-redis-key-7f3a",
	"canary-user",
	"canary-pass",
	"canary.example",
	"canary-fragment",
	"canary-topic-9f8e7d6c",
	"canary-payload-b1a2c3",
	"canary-email-4d5e6f@example.com",
}

// sensitiveDocument carries canary values in every Phase 1-blocked
// dependency field.
func sensitiveDocument() *observabilityv1.ObservabilityDocument {
	return &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "sensitive"},
		Functions:     []*observabilityv1.Function{{Id: "fn:f", QualifiedName: "sensitive.F"}},
		Endpoints: []*observabilityv1.Endpoint{{
			Id: "ep:http", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
			Name: "process", FunctionId: "fn:f", HttpMethod: "POST", HttpPath: "/process",
		}},
		Dependencies: []*observabilityv1.Dependency{
			{
				Id: "dep:sql", Kind: observabilityv1.DependencyKind_SQL,
				Name: "Store", FunctionId: "fn:f", Operation: "query",
				Value: "SELECT * FROM users WHERE password = 'canary-password-hunter2'", ValueIsStatic: true,
			},
			{
				Id: "dep:redis", Kind: observabilityv1.DependencyKind_REDIS,
				Name: "Cache", FunctionId: "fn:f", Operation: "get",
				Value: "user:42:session:canary-redis-key-7f3a", ValueIsStatic: true,
			},
			{
				Id: "dep:http", Kind: observabilityv1.DependencyKind_HTTP_CLIENT,
				Name: "External", FunctionId: "fn:f", Operation: "GET",
				TargetUrl: "https://canary-user:canary-pass@canary.example/orders?id=42#canary-fragment", ValueIsStatic: true,
			},
			{
				Id: "dep:producer", Kind: observabilityv1.DependencyKind_KAFKA_PRODUCER,
				Name: "Events", FunctionId: "fn:f", Operation: "produce",
				Resource: "canary-topic-9f8e7d6c", Value: "{\"event\":\"canary-payload-b1a2c3\"}", ValueIsStatic: true,
			},
		},
		Diagnostics: []*observabilityv1.Diagnostic{{
			Severity: observabilityv1.DiagnosticSeverity_WARNING,
			Code:     "ANALYZER_SENSITIVE_VALUE",
			Message:  "sensitive value canary-email-4d5e6f@example.com detected",
		}},
	}
}

// TestCatalogCanaryAC4: sensitive values never enter the catalog, its
// diagnostics or its errors, across every serialization.
func TestCatalogCanaryAC4(t *testing.T) {
	document := sensitiveDocument()
	plan, err := planDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	catalog, err := BuildCatalog(document, plan, Policy{IncludeClientDependencies: true})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	contents, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var builder strings.Builder
	builder.Write(contents)
	for _, diagnostic := range catalog.Diagnostics {
		builder.WriteString(" " + diagnostic.Message)
	}
	all := builder.String()
	for _, canary := range canaries {
		if strings.Contains(all, canary) {
			t.Fatalf("canary %q leaked into the catalog", canary)
		}
	}
}

// TestCatalogPermutationAC5: 25 fixed random permutations of the
// composite IR produce semantically equal catalogs.
func TestCatalogPermutationAC5(t *testing.T) {
	base := compositeDocument()
	basePlan, err := planDocument(t, base)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	baseline, err := BuildCatalog(base, basePlan, Policy{IncludeClientDependencies: true})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	rng := rand.New(rand.NewSource(20260810))
	for iteration := range 25 {
		document := compositeDocument()
		rng.Shuffle(len(document.Functions), func(i, j int) {
			document.Functions[i], document.Functions[j] = document.Functions[j], document.Functions[i]
		})
		rng.Shuffle(len(document.Endpoints), func(i, j int) {
			document.Endpoints[i], document.Endpoints[j] = document.Endpoints[j], document.Endpoints[i]
		})
		rng.Shuffle(len(document.Dependencies), func(i, j int) {
			document.Dependencies[i], document.Dependencies[j] = document.Dependencies[j], document.Dependencies[i]
		})
		plan, err := planDocument(t, document)
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		catalog, err := BuildCatalog(document, plan, Policy{IncludeClientDependencies: true})
		if err != nil {
			t.Fatalf("permutation %d: %v", iteration, err)
		}
		if !reflect.DeepEqual(catalog, baseline) {
			t.Fatalf("permutation %d changed the catalog", iteration)
		}
	}
}

// TestCatalogStrictMode: warnings (e.g. an unsupported target) fail the
// build under strict policy.
func TestCatalogStrictMode(t *testing.T) {
	document := compositeDocument()
	document.Endpoints = append(document.Endpoints, &observabilityv1.Endpoint{
		Id: "ep:unknown", Kind: observabilityv1.EndpointKind_ENDPOINT_KIND_UNSPECIFIED,
		Name: "mystery", FunctionId: "fn:http",
	})
	plan, err := planDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := BuildCatalog(document, plan, Policy{IncludeClientDependencies: true}); err != nil {
		t.Fatalf("non-strict build must keep warnings: %v", err)
	}
	_, err = BuildCatalog(document, plan, Policy{IncludeClientDependencies: true, Strict: true})
	if err == nil {
		t.Fatal("strict build must fail on warnings")
	}
}

func planDocument(t *testing.T, document *observabilityv1.ObservabilityDocument) (*observabilityv1.GenerationPlan, error) {
	t.Helper()
	resolved, err := policy.Resolve(nil, nil)
	if err != nil {
		t.Fatalf("policy: %v", err)
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

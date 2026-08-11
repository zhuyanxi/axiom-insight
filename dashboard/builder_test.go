package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/planner"
	"github.com/zhuyanxi/axiom-insight/generator/planner/logging"
	"github.com/zhuyanxi/axiom-insight/generator/planner/metrics"
	"github.com/zhuyanxi/axiom-insight/generator/planner/tracing"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// compositeDocument covers all endpoint and dependency kinds.
func compositeDocument() *observabilityv1.ObservabilityDocument {
	document := &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "checkout"},
		Functions: []*observabilityv1.Function{
			{Id: "fn:http", QualifiedName: "checkout.HandleOrder", PackagePath: "internal/orders"},
			{Id: "fn:grpc", QualifiedName: "checkout.Server", PackagePath: "internal/orders"},
			{Id: "fn:cron", QualifiedName: "checkout.Cleanup", PackagePath: "internal/maintenance"},
		},
		Endpoints: []*observabilityv1.Endpoint{
			{Id: "ep:http", Kind: observabilityv1.EndpointKind_HTTP_HANDLER, Name: "createOrder", FunctionId: "fn:http", HttpMethod: "POST", HttpPath: "/orders/{id}"},
			{Id: "ep:grpc", Kind: observabilityv1.EndpointKind_GRPC_HANDLER, Name: "createOrder", FunctionId: "fn:grpc", GrpcService: "OrderService", GrpcMethod: "CreateOrder"},
			{Id: "ep:cron", Kind: observabilityv1.EndpointKind_CRON_JOB, Name: "cleanup", FunctionId: "fn:cron", CronSchedule: "0 3 * * *"},
		},
		Dependencies: []*observabilityv1.Dependency{
			{Id: "dep:producer", Kind: observabilityv1.DependencyKind_KAFKA_PRODUCER, Name: "Orders", FunctionId: "fn:grpc", Operation: "produce", ValueIsStatic: true},
			{Id: "dep:consumer", Kind: observabilityv1.DependencyKind_KAFKA_CONSUMER, Name: "Orders", FunctionId: "fn:cron", Operation: "consume", ValueIsStatic: true},
			{Id: "dep:sql", Kind: observabilityv1.DependencyKind_SQL, Name: "Store", FunctionId: "fn:http", Operation: "query", ValueIsStatic: true},
			{Id: "dep:redis", Kind: observabilityv1.DependencyKind_REDIS, Name: "Cache", FunctionId: "fn:http", Operation: "get", ValueIsStatic: true},
			{Id: "dep:http-client", Kind: observabilityv1.DependencyKind_HTTP_CLIENT, Name: "Payments", FunctionId: "fn:http", Operation: "GET", ValueIsStatic: true},
			{Id: "dep:rpc-client", Kind: observabilityv1.DependencyKind_RPC_CLIENT, Name: "Inventory", FunctionId: "fn:grpc", Operation: "Reserve", ValueIsStatic: true},
		},
	}
	return document
}

// plannedDocument runs the Phase 1 pipeline over the document.
func plannedDocument(t *testing.T, document *observabilityv1.ObservabilityDocument) (*observabilityv1.GenerationPlan, error) {
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
		return nil, err
	}
	return plan, nil
}

func buildCompositeCatalog(t *testing.T) *DashboardCatalog {
	t.Helper()
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	catalog, err := BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: true})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	return catalog
}

func itemFor(catalog *DashboardCatalog, targetID string) *DashboardItem {
	for index := range catalog.Items {
		if catalog.Items[index].Target.ID == targetID {
			return &catalog.Items[index]
		}
	}
	return nil
}

// TestCatalogKindsAC1: every supported entity kind produces a stable item
// whose metric/span references point back at valid plan IDs for the same
// target.
func TestCatalogKindsAC1(t *testing.T) {
	catalog := buildCompositeCatalog(t)
	wantCategories := map[string]Category{
		"ep:http":       CategoryHTTP,
		"ep:grpc":       CategoryRPC,
		"ep:cron":       CategoryServiceOverview,
		"dep:producer":  CategoryKafka,
		"dep:consumer":  CategoryKafka,
		"dep:sql":       CategoryDatabase,
		"dep:redis":     CategoryCache,
		"dep:http-client": CategoryHTTP,
		"dep:rpc-client":  CategoryRPC,
	}
	for targetID, category := range wantCategories {
		item := itemFor(catalog, targetID)
		if item == nil {
			t.Errorf("missing item for %s", targetID)
			continue
		}
		if item.Category != category {
			t.Errorf("%s category = %s, want %s", targetID, item.Category, category)
		}
		if item.ID != "item:"+string(category)+":"+targetID {
			t.Errorf("%s ID = %q", targetID, item.ID)
		}
		if len(item.Metrics) == 0 || len(item.Spans) == 0 {
			t.Errorf("%s lacks metric/span references", targetID)
		}
		for _, metric := range item.Metrics {
			if metric.PlanID == "" || metric.Name == "" {
				t.Errorf("%s has an empty metric reference", targetID)
			}
		}
		for _, span := range item.Spans {
			if span.PlanID == "" || span.Name == "" {
				t.Errorf("%s has an empty span reference", targetID)
			}
		}
	}
	// Items are sorted by (category, kind, ID).
	for index := 1; index < len(catalog.Items); index++ {
		previous, current := catalog.Items[index-1], catalog.Items[index]
		if previous.Category > current.Category ||
			(previous.Category == current.Category && previous.Target.ID > current.Target.ID) {
			t.Errorf("items not sorted at %d: %s before %s", index, previous.ID, current.ID)
		}
	}
}

// TestCatalogCapabilitiesAC3: an item with only a counter and no
// histogram, gauge, status label or span has only rate available, each
// other capability explaining why not.
func TestCatalogCapabilitiesAC3(t *testing.T) {
	document := &observabilityv1.ObservabilityDocument{
		SchemaVersion: "v1",
		Service:       &observabilityv1.Service{Name: "minimal"},
		Functions:     []*observabilityv1.Function{{Id: "fn:f", QualifiedName: "minimal.F"}},
		Endpoints: []*observabilityv1.Endpoint{{
			Id: "ep:min", Kind: observabilityv1.EndpointKind_HTTP_HANDLER,
			Name: "min", FunctionId: "fn:f", HttpMethod: "GET", HttpPath: "/min",
		}},
	}
	plan := &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: "v1",
		ServiceName:           "minimal",
		Metrics: []*observabilityv1.MetricPlan{{
			Id: "metrics:ep:min:count", Name: "min_requests_total",
			Type:   observabilityv1.MetricType_METRIC_TYPE_COUNTER,
			Target: &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: "ep:min"},
			Attributes: []*observabilityv1.AttributeBinding{
				{Key: "service", Value: &observabilityv1.ValueBinding{Path: "service.name"}},
				{Key: "operation", Value: &observabilityv1.ValueBinding{Path: "plan.constant.get"}},
			},
		}},
	}
	catalog, err := BuildCatalog(document, plan, DashboardPolicy{})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	item := itemFor(catalog, "ep:min")
	if item == nil {
		t.Fatal("missing item")
	}
	capabilities := item.Capabilities
	if !capabilities.Rate.Available {
		t.Error("rate must be available with a counter + service/operation")
	}
	for _, check := range []struct {
		name string
		got  QueryCapability
	}{
		{"error ratio", capabilities.ErrorRatio},
		{"percentiles", capabilities.Percentiles},
		{"in flight", capabilities.InFlight},
		{"trace link", capabilities.TraceLink},
	} {
		if check.got.Available {
			t.Errorf("%s must be unavailable", check.name)
		}
		if check.got.Reason == "" {
			t.Errorf("%s must explain its unavailability", check.name)
		}
	}
}

// TestCatalogCapabilitiesFull: the composite item has all five
// capabilities available.
func TestCatalogCapabilitiesFull(t *testing.T) {
	catalog := buildCompositeCatalog(t)
	for _, targetID := range []string{"ep:http", "dep:sql"} {
		item := itemFor(catalog, targetID)
		if item == nil {
			t.Fatalf("missing %s", targetID)
		}
		for _, check := range []struct {
			name string
			got  QueryCapability
		}{
			{"rate", item.Capabilities.Rate},
			{"error ratio", item.Capabilities.ErrorRatio},
			{"percentiles", item.Capabilities.Percentiles},
			{"in flight", item.Capabilities.InFlight},
			{"trace link", item.Capabilities.TraceLink},
		} {
			if !check.got.Available {
				t.Errorf("%s.%s must be available: %s", targetID, check.name, check.got.Reason)
			}
		}
	}
}

// TestCatalogRejectsInvalidReferencesAC2: dangling and mismatched plan
// references fail with DASHBOARD_DANGLING_REFERENCE and no partial
// catalog.
func TestCatalogRejectsInvalidReferencesAC2(t *testing.T) {
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Point one metric at a dependency ID that does not exist.
	plan.Metrics[0].Target = &observabilityv1.TargetRef{
		Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: "dep:missing",
	}
	catalog, err := BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: true})
	if err == nil {
		t.Fatal("dangling reference must fail")
	}
	var failures *CatalogErrors
	if !errors.As(err, &failures) {
		t.Fatalf("error type = %T", err)
	}
	found := false
	for _, violation := range failures.Violations() {
		if violation.Code == CodeDanglingReference && strings.Contains(violation.Message, "dep:missing") {
			found = true
		}
	}
	if !found {
		t.Errorf("violations = %v", failures.Violations())
	}
	if catalog != nil {
		t.Fatal("no partial catalog may be returned")
	}
}

// TestCatalogRejectsKindMismatchAC2: a metric referencing an endpoint ID
// with a dependency kind fails.
func TestCatalogRejectsKindMismatchAC2(t *testing.T) {
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	plan.Metrics[0].Target = &observabilityv1.TargetRef{
		Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: "ep:http",
	}
	_, err = BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: true})
	var failures *CatalogErrors
	if !errors.As(err, &failures) {
		t.Fatalf("error type = %T", err)
	}
	for _, violation := range failures.Violations() {
		if violation.Code == CodeDanglingReference {
			return
		}
	}
	t.Errorf("violations = %v", failures.Violations())
}

// TestCatalogRejectsNilAndBadSchema: nil inputs and unsupported schemas
// fail with the right codes.
func TestCatalogRejectsNilAndBadSchema(t *testing.T) {
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := BuildCatalog(nil, plan, DashboardPolicy{}); err == nil ||
		!strings.Contains(err.Error(), CodeInvalidIR) {
		t.Fatalf("nil document must fail with %s", CodeInvalidIR)
	}
	if _, err := BuildCatalog(document, nil, DashboardPolicy{}); err == nil ||
		!strings.Contains(err.Error(), CodeInvalidIR) {
		t.Fatalf("nil plan must fail with %s", CodeInvalidIR)
	}
	badDocument := compositeDocument()
	badDocument.SchemaVersion = "v99"
	if _, err := BuildCatalog(badDocument, plan, DashboardPolicy{}); err == nil ||
		!strings.Contains(err.Error(), CodeUnsupportedSchema) {
		t.Fatalf("unsupported IR schema must fail with %s", CodeUnsupportedSchema)
	}
	badPlan, _ := plannedDocument(t, document)
	badPlan.SchemaVersion = "v99"
	if _, err := BuildCatalog(document, badPlan, DashboardPolicy{}); err == nil ||
		!strings.Contains(err.Error(), CodeUnsupportedSchema) {
		t.Fatalf("unsupported plan schema must fail with %s", CodeUnsupportedSchema)
	}
}

// TestCatalogClientDependenciesPolicy: client dependencies enter the
// catalog only when the policy allows them.
func TestCatalogClientDependenciesPolicy(t *testing.T) {
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	without, err := BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: false})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if itemFor(without, "dep:http-client") != nil || itemFor(without, "dep:rpc-client") != nil {
		t.Error("client dependencies must be excluded when the policy disables them")
	}
	with, err := BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: true})
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if itemFor(with, "dep:http-client") == nil || itemFor(with, "dep:rpc-client") == nil {
		t.Error("client dependencies must be included when the policy enables them")
	}
}

// TestCatalogDoesNotMutateInput: the input document and plan are
// byte-identical after building.
func TestCatalogDoesNotMutateInput(t *testing.T) {
	document := compositeDocument()
	plan, err := plannedDocument(t, document)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	documentBefore, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	planBefore, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if _, err := BuildCatalog(document, plan, DashboardPolicy{IncludeClientDependencies: true}); err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	documentAfter, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	planAfter, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if !reflect.DeepEqual(documentBefore, documentAfter) || !reflect.DeepEqual(planBefore, planAfter) {
		t.Fatal("builder mutated its inputs")
	}
}

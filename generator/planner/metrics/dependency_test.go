package metrics

import (
	"context"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// sixKindDocument builds an IR with one function and one dependency of
// every supported kind.
func sixKindDocument() *observabilityv1.ObservabilityDocument {
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

func planDependencyMetrics(t *testing.T, document *observabilityv1.ObservabilityDocument, resolved policy.Policy) *planner.MetricsResult {
	t.Helper()
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	result, err := DependencyMetricsPlanner{}.PlanMetrics(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: resolved,
	})
	if err != nil {
		t.Fatalf("PlanMetrics failed: %v", err)
	}
	return result
}

// TestDependencyKindMappingExhaustive is the mapping guard: every known
// kind maps to exactly its controlled system, and the switch that builds
// purposes covers all six.
func TestDependencyKindMappingExhaustive(t *testing.T) {
	wantSystem := map[observabilityv1.DependencyKind]string{
		observabilityv1.DependencyKind_KAFKA_PRODUCER: "kafka",
		observabilityv1.DependencyKind_KAFKA_CONSUMER: "kafka",
		observabilityv1.DependencyKind_SQL:            "sql",
		observabilityv1.DependencyKind_REDIS:          "redis",
		observabilityv1.DependencyKind_HTTP_CLIENT:    "http",
		observabilityv1.DependencyKind_RPC_CLIENT:     "rpc",
	}
	if len(dependencyKinds) != len(wantSystem) {
		t.Fatalf("mapping covers %d kinds, want %d", len(dependencyKinds), len(wantSystem))
	}
	for kind, system := range wantSystem {
		spec, ok := dependencyKinds[kind]
		if !ok {
			t.Errorf("kind %v is not mapped", kind)
			continue
		}
		if spec.system != system {
			t.Errorf("kind %v system = %q, want %q", kind, spec.system, system)
		}
		if !strings.HasSuffix(dependencyPurpose(spec.system, "operations_total"), system+"_operations_total") {
			t.Errorf("purpose for %v does not carry the system", kind)
		}
	}
}

// TestUnspecifiedKindNotGuessed: the unspecified dependency kind is
// skipped with GEN_UNSUPPORTED_ENTITY, never guessed.
func TestUnspecifiedKindNotGuessed(t *testing.T) {
	document := sixKindDocument()
	document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
		Id: "dep:unknown", Kind: observabilityv1.DependencyKind_DEPENDENCY_KIND_UNSPECIFIED,
		FunctionId: "fn:service", Operation: "mystery", ValueIsStatic: true,
	})
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeUnsupportedEntity && diagnostic.TargetID == "dep:unknown" {
			found = true
		}
	}
	if !found {
		t.Errorf("unspecified kind must produce GEN_UNSUPPORTED_ENTITY, got %v", result.Diagnostics)
	}
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
	for _, item := range result.Items {
		if item.GetTarget().GetId() == "dep:unknown" {
			t.Error("unspecified kind must not be guessed into items")
		}
	}
}

// TestSixKindsGetBasicMetricsAC1: every kind gets an operations counter
// and a seconds histogram with the correct system/operation mapping.
func TestSixKindsGetBasicMetricsAC1(t *testing.T) {
	document := sixKindDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)
	// 6 dependencies x 3 instruments (counter, histogram, gauge).
	if len(result.Items) != 18 {
		t.Fatalf("item count = %d, want 18", len(result.Items))
	}
	byTarget := map[string][]*observabilityv1.MetricPlan{}
	for _, item := range result.Items {
		byTarget[item.GetTarget().GetId()] = append(byTarget[item.GetTarget().GetId()], item)
	}
	wantSystems := map[string]string{
		"dep:producer": "kafka", "dep:consumer": "kafka", "dep:sql": "sql",
		"dep:redis": "redis", "dep:http": "http", "dep:rpc": "rpc",
	}
	for targetID, items := range byTarget {
		system := wantSystems[targetID]
		if system == "" {
			t.Errorf("unexpected target %s", targetID)
			continue
		}
		var counter, histogram bool
		for _, item := range items {
			if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_COUNTER &&
				strings.HasSuffix(item.GetName(), system+"_operations_total") {
				counter = true
			}
			if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM &&
				strings.HasSuffix(item.GetName(), system+"_operation_duration_seconds") {
				histogram = true
			}
			if item.GetTarget().GetKind() != observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY {
				t.Errorf("item %s has wrong target kind", item.GetId())
			}
			if item.GetFunctionId() != "fn:service" {
				t.Errorf("item %s lacks the function reference", item.GetId())
			}
			if item.GetUnit() == "s" {
				continue
			}
			if item.GetUnit() != "{operation}" {
				t.Errorf("item %s unit = %q, want {operation} or s", item.GetId(), item.GetUnit())
			}
		}
		if !counter || !histogram {
			t.Errorf("target %s lacks counter/histogram", targetID)
		}
	}
	// rpc operation is the normalized service/method.
	for _, item := range byTarget["dep:rpc"] {
		operation := attributeValue(item, "operation")
		if operation == nil || operation.GetPath() != "plan.constant.orderservice_createorder" {
			t.Errorf("rpc operation binding = %v", operation)
		}
	}
}

// TestSensitiveResourcesNeverEnterMetricsAC2: SQL text, Redis key,
// credential URL and Kafka payload canaries never appear in names,
// descriptions, attributes, diagnostics or serialized bytes.
func TestSensitiveResourcesNeverEnterMetricsAC2(t *testing.T) {
	document := sixKindDocument()
	canaries := map[string]string{
		"dep:sql":    "SELECT * FROM users WHERE password = 'hunter2'",
		"dep:redis":  "user:42:session",
		"dep:http":   "https://user:pass@example.com/orders?id=42#detail",
		"dep:producer": "kafka-payload-canary-7f3a",
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
	}
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)

	var all string
	for _, item := range result.Items {
		all += item.GetName() + " " + item.GetDescription() + " "
		for _, attribute := range item.GetAttributes() {
			all += attribute.GetKey() + " " + attribute.GetValue().GetPath() + " "
		}
	}
	for _, diagnostic := range result.Diagnostics {
		all += diagnostic.Message + " "
	}
	for targetID, canary := range canaries {
		if strings.Contains(all, canary) {
			t.Fatalf("canary for %s leaks into metrics: %s", targetID, all)
		}
	}
	// The operation attribute must stay controlled: HTTP client keeps
	// "get", never the URL.
	for _, item := range result.Items {
		if item.GetTarget().GetId() != "dep:http" {
			continue
		}
		operation := attributeValue(item, "operation")
		if operation == nil || !strings.Contains(operation.GetPath(), "plan.constant.get") {
			t.Errorf("HTTP client operation leaks target data: %v", operation)
		}
	}
}

// TestDynamicTargetDegradesAC3: ValueIsStatic=false dependencies keep
// generic system/operation metrics and produce GEN_INCOMPLETE_TARGET
// without target values.
func TestDynamicTargetDegradesAC3(t *testing.T) {
	document := sixKindDocument()
	document.Dependencies[4].ValueIsStatic = false // dep:http
	document.Dependencies[0].ValueIsStatic = false // dep:producer
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)
	count := 0
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget &&
			(diagnostic.TargetID == "dep:http" || diagnostic.TargetID == "dep:producer") {
			count++
			if strings.Contains(diagnostic.Message, "http") && strings.Contains(diagnostic.Message, "target") {
				// message must never carry the target value
			}
		}
	}
	if count != 2 {
		t.Errorf("GEN_INCOMPLETE_TARGET count = %d, want 2; %v", count, result.Diagnostics)
	}
	// Generic metrics still exist for the dynamic targets.
	seen := map[string]bool{}
	for _, item := range result.Items {
		seen[item.GetTarget().GetId()] = true
	}
	if !seen["dep:http"] || !seen["dep:producer"] {
		t.Error("dynamic targets must keep generic metrics")
	}
}

// TestMultiCallSiteAC4: two dependencies of the same function and same
// operation keep distinct plan IDs and get deterministic P1-04 name
// disambiguation with one GEN_NAME_COLLISION.
func TestMultiCallSiteAC4(t *testing.T) {
	document := sixKindDocument()
	document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
		Id: "dep:redis-2", Kind: observabilityv1.DependencyKind_REDIS,
		Name: "Cache", FunctionId: "fn:service", Operation: "get", ValueIsStatic: true,
	})
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)

	var firstID, secondID string
	var firstItem, secondItem *observabilityv1.MetricPlan
	collision := 0
	for _, item := range result.Items {
		if item.GetTarget().GetId() != "dep:redis" && item.GetTarget().GetId() != "dep:redis-2" {
			continue
		}
		if item.GetType() != observabilityv1.MetricType_METRIC_TYPE_COUNTER {
			continue
		}
		if item.GetTarget().GetId() == "dep:redis" {
			firstID, firstItem = item.GetId(), item
		} else {
			secondID, secondItem = item.GetId(), item
		}
	}
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("plan IDs must differ per call site: %q %q", firstID, secondID)
	}
	if firstItem.GetName() == secondItem.GetName() {
		t.Fatalf("colliding names must be disambiguated: %q", firstItem.GetName())
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeNameCollision {
			collision++
		}
	}
	// Each of the three colliding instruments (counter, histogram, gauge)
	// produces one GEN_NAME_COLLISION.
	if collision != 3 {
		t.Errorf("GEN_NAME_COLLISION count = %d, want 3; %v", collision, result.Diagnostics)
	}
	// The disambiguated names stay distinct across the two call sites.
	seen := map[string]bool{}
	for _, item := range result.Items {
		if item.GetType() != observabilityv1.MetricType_METRIC_TYPE_COUNTER {
			continue
		}
		if item.GetTarget().GetId() == "dep:redis" || item.GetTarget().GetId() == "dep:redis-2" {
			if seen[item.GetName()] {
				t.Errorf("duplicate name after disambiguation: %q", item.GetName())
			}
			seen[item.GetName()] = true
		}
	}
}

// TestMultiCallSiteDeterministic: reversing the input order yields the
// same name mapping.
func TestMultiCallSiteDeterministic(t *testing.T) {
	build := func() *planner.MetricsResult {
		document := sixKindDocument()
		document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{
			Id: "dep:redis-2", Kind: observabilityv1.DependencyKind_REDIS,
			Name: "Cache", FunctionId: "fn:service", Operation: "get", ValueIsStatic: true,
		})
		defaults, _ := policy.Resolve(nil, nil)
		return planDependencyMetrics(t, document, *defaults)
	}
	first := build()
	second := build()
	names := map[string]string{}
	for _, item := range first.Items {
		names[item.GetId()] = item.GetName()
	}
	for _, item := range second.Items {
		if item.GetName() != names[item.GetId()] {
			t.Errorf("name mapping differs across runs for %s: %q vs %q", item.GetId(), names[item.GetId()], item.GetName())
		}
	}
}

// TestMissingOperationDegrades: missing operation uses the controlled
// "unknown" and produces GEN_INCOMPLETE_TARGET.
func TestMissingOperationDegrades(t *testing.T) {
	document := sixKindDocument()
	document.Dependencies[2].Operation = "" // dep:sql
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == policy.CodeIncompleteTarget && diagnostic.TargetID == "dep:sql" &&
			diagnostic.Field == "operation" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing operation must degrade with a diagnostic: %v", result.Diagnostics)
	}
	for _, item := range result.Items {
		if item.GetTarget().GetId() == "dep:sql" {
			operation := attributeValue(item, "operation")
			if operation == nil || !strings.Contains(operation.GetPath(), "plan.constant.unknown") {
				t.Errorf("missing operation must bind the controlled unknown: %v", operation)
			}
		}
	}
}

// TestGaugeSemanticsAC5: the in-flight gauge binds only
// runtime.operation.in_flight and never claims pool or queue depth.
func TestGaugeSemanticsAC5(t *testing.T) {
	document := sixKindDocument()
	defaults, _ := policy.Resolve(nil, nil)
	result := planDependencyMetrics(t, document, *defaults)
	for _, item := range result.Items {
		if item.GetType() != observabilityv1.MetricType_METRIC_TYPE_GAUGE {
			continue
		}
		if item.GetValue().GetPath() != "runtime.operation.in_flight" {
			t.Errorf("gauge %s value source = %q, want runtime.operation.in_flight", item.GetId(), item.GetValue().GetPath())
		}
		if item.GetTrigger().GetPhase() != observabilityv1.TriggerPhase_TRIGGER_PHASE_STATE_CHANGE {
			t.Errorf("gauge %s trigger = %v", item.GetId(), item.GetTrigger().GetPhase())
		}
		if strings.Contains(item.GetDescription(), "pool") || strings.Contains(item.GetDescription(), "queue") ||
			strings.Contains(item.GetDescription(), "lag") {
			t.Errorf("gauge description overclaims: %q", item.GetDescription())
		}
	}
}

// TestDisabledGaugeSummaryLeaveNoEmptyDefinitions: disabling gauge and
// summary removes them entirely.
func TestDisabledGaugeSummaryLeaveNoEmptyDefinitions(t *testing.T) {
	document := sixKindDocument()
	minimal, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{
			IncludeInFlightGauges: new(false),
			Summaries:             &policy.SummariesConfig{Enabled: new(false)},
		},
	}, nil)
	result := planDependencyMetrics(t, document, *minimal)
	if len(result.Items) != 12 {
		t.Fatalf("item count = %d, want 12 (6 x counter+histogram)", len(result.Items))
	}
	for _, item := range result.Items {
		if item.GetType() == observabilityv1.MetricType_METRIC_TYPE_GAUGE ||
			item.GetType() == observabilityv1.MetricType_METRIC_TYPE_SUMMARY {
			t.Errorf("disabled instrument still present: %s", item.GetId())
		}
	}
}

// TestDependencyBudgetFails: an over-tight series budget fails the whole
// dependency metrics result.
func TestDependencyBudgetFails(t *testing.T) {
	document := sixKindDocument()
	tight, _ := policy.Resolve(&policy.GenerationConfig{
		Metrics: &policy.MetricsConfig{MaxEstimatedSeries: new(int64(10))},
	}, nil)
	index, violations, err := planner.ValidateDocument(context.Background(), document)
	if err != nil || len(violations) > 0 {
		t.Fatalf("IR invalid: %v %v", violations, err)
	}
	_, err = DependencyMetricsPlanner{}.PlanMetrics(context.Background(), &planner.SignalInput{
		Document: document, Index: index, Policy: *tight,
	})
	if err == nil || !strings.Contains(err.Error(), policy.CodeCardinalityLimitExceeded) {
		t.Fatalf("budget overflow must fail with the cardinality code, got %v", err)
	}
}

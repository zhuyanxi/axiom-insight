package category

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

func kafkaItem(id, dependencyKind, operation string, metrics []dashboard.SignalReference, spans []dashboard.SignalReference) dashboard.DashboardItem {
	item := dashboard.DashboardItem{
		ID: id, Category: dashboard.CategoryKafka,
		DependencyKind: dependencyKind,
		Target:         dashboard.TargetRef{Kind: "dependency", ID: id},
		Operation:      operation,
		Metrics:        metrics,
		Spans:          spans,
	}
	item.Capabilities = capabilitiesOf(metrics, spans)
	return item
}

func fullKafkaProducer(id, operation string) dashboard.DashboardItem {
	return kafkaItem(id, "kafka_producer", operation,
		[]dashboard.SignalReference{
			metric(id+":count", "kafka_messages_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "kafka_message_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Publish/"+operation)})
}

func fullKafkaConsumer(id, operation string) dashboard.DashboardItem {
	return kafkaItem(id, "kafka_consumer", operation,
		[]dashboard.SignalReference{
			metric(id+":count", "kafka_messages_total", "counter", "service", "operation", "status"),
			metric(id+":dur", "kafka_message_duration", "histogram", "service", "operation"),
		},
		[]dashboard.SignalReference{span(id+":span", "Consume/"+operation)})
}

func kafkaCounterOnly(id, dependencyKind, operation string) dashboard.DashboardItem {
	return kafkaItem(id, dependencyKind, operation,
		[]dashboard.SignalReference{
			metric(id+":count", "kafka_messages_total", "counter", "service", "operation", "status"),
		}, nil)
}

func kafkaCatalog() *dashboard.DashboardCatalog {
	return &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			fullKafkaProducer("dep:kafka:orders", "produce"),
			fullKafkaProducer("dep:kafka:audit", "emit"),
			fullKafkaConsumer("dep:kafka:orders_consumer", "consume"),
		},
	}
}

// TestBuildKafkaFull is AC1: producer and consumer classes generate rate,
// error ratio, duration percentiles and trace links; queries reference
// only plan metrics and labels.
func TestBuildKafkaFull(t *testing.T) {
	plan, err := BuildKafka(kafkaCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].Category != dashboard.CategoryKafka {
		t.Fatalf("rows = %+v, want one kafka row", plan.Rows)
	}
	// producer: 6 panels, consumer: 6 panels.
	if len(plan.Rows[0].Panels) != 12 {
		t.Fatalf("kafka panels = %d, want 12", len(plan.Rows[0].Panels))
	}
	var rateTargets int
	for _, panel := range plan.Rows[0].Panels {
		if panel.Purpose == "rate" {
			rateTargets++
			if len(panel.Links) == 0 {
				t.Errorf("rate panel lacks trace link: %s", panel.Key)
			}
		}
		if !strings.HasPrefix(panel.Title, "Kafka producer ") && !strings.HasPrefix(panel.Title, "Kafka consumer ") {
			t.Errorf("panel title lacks controlled class subtitle: %q", panel.Title)
		}
		for _, target := range panel.Targets {
			for _, forbidden := range []string{"topic", "group", "partition", "offset", "payload"} {
				if strings.Contains(target.Expr, forbidden) {
					t.Errorf("query references forbidden Kafka dimension %q: %s", forbidden, target.Expr)
				}
			}
		}
	}
	if rateTargets != 2 {
		t.Errorf("rate panels = %d, want 2 (producer + consumer)", rateTargets)
	}

	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rendered rows = %d, want 1", len(rows))
	}
	validateRenderedDashboard(t, rows)
}

// TestBuildKafkaDegradation covers missing-histogram fixtures: only
// provable rate/error/table panels appear, percentile panels are omitted
// with diagnostics.
func TestBuildKafkaDegradation(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items: []dashboard.DashboardItem{
			kafkaCounterOnly("dep:kafka:orders", "kafka_producer", "produce"),
			kafkaCounterOnly("dep:kafka:orders_consumer", "kafka_consumer", "consume"),
		},
	}
	plan, err := BuildKafka(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	if len(plan.Rows) != 1 || len(plan.Rows[0].Panels) != 6 {
		t.Fatalf("kafka panels = %d, want 6 (rate/error/table per class)", len(plan.Rows[0].Panels))
	}
	for _, panel := range plan.Rows[0].Panels {
		if strings.Contains(panel.Purpose, "p5") || panel.Purpose == "in_flight" {
			t.Errorf("unsupported panel generated: %s", panel.Purpose)
		}
		if len(panel.Links) != 0 {
			t.Errorf("trace link generated without span plan: %s", panel.Key)
		}
	}
	found := false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeMissingRequiredMetric && strings.Contains(diagnostic.Field, "p50") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected percentile degradation diagnostics, got %v", plan.Diagnostics)
	}
}

// TestBuildKafkaNoEntities is AC3: with no Kafka entities the row is
// omitted and both DASHBOARD_EMPTY_CATEGORY and the catalog's
// DASHBOARD_UNSUPPORTED_TARGET diagnostics are preserved.
func TestBuildKafkaNoEntities(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Diagnostics: []dashboard.Diagnostic{{
			Code: dashboard.CodeUnsupportedTarget, TargetID: "dep:dynamic:kafka",
			Field:   "dependencies[dep:dynamic:kafka].kind",
			Message: "dependency kind has no safe v1 dashboard mapping",
		}},
	}
	plan, err := BuildKafka(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	if len(plan.Rows) != 1 || len(plan.Rows[0].Panels) != 0 {
		t.Fatalf("expected an omitted kafka row, got %+v", plan.Rows)
	}
	foundEmpty, foundUnsupported := false, false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeEmptyCategory {
			foundEmpty = true
		}
		if diagnostic.Code == dashboard.CodeUnsupportedTarget {
			foundUnsupported = true
		}
	}
	if !foundEmpty || !foundUnsupported {
		t.Errorf("diagnostics = %v, want empty-category and unsupported-target", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty kafka row must be omitted, got %d rows", len(rows))
	}
}

// TestKafkaCanary is AC2: topic/group/payload-like canaries and raw
// values never reach output, queries or errors; no lag/offset/partition
// panels are generated.
func TestKafkaCanary(t *testing.T) {
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   `payment";drop`,
		Items: []dashboard.DashboardItem{
			kafkaItem("dep:kafka:orders", "kafka_producer", "produce",
				[]dashboard.SignalReference{
					metric("m_bad", `kafka_messages_total";alert(1)`, "counter", "service", "operation", "status"),
					metric("m_ok", "kafka_messages_total", "counter", "service", "operation", "status"),
				},
				[]dashboard.SignalReference{span("s_bad", `Publish";drop`)}),
			kafkaItem("dep:kafka:bad_op", "kafka_producer", `emit";op`,
				[]dashboard.SignalReference{
					metric("m_op", "kafka_messages_op_total", "counter", "service", "operation", "status"),
				},
				[]dashboard.SignalReference{span("s_op", "Publish/emit")}),
			fullKafkaConsumer("dep:kafka:orders_consumer", "consume"),
		},
	}
	plan, err := BuildKafka(catalog, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	canaries := []string{`alert(1)`, `";drop`}
	for _, diagnostic := range plan.Diagnostics {
		for _, canary := range canaries {
			if strings.Contains(diagnostic.Message, canary) || strings.Contains(diagnostic.Field, canary) {
				t.Errorf("diagnostic leaks canary %q: %+v", canary, diagnostic)
			}
		}
	}
	foundSpanDrop, foundOperationDrop := false, false
	for _, diagnostic := range plan.Diagnostics {
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped && diagnostic.Field == "spans[].name" {
			foundSpanDrop = true
		}
		if diagnostic.Code == dashboard.CodeSensitiveValueDropped && diagnostic.Field == "operation" {
			foundOperationDrop = true
		}
	}
	if !foundSpanDrop || !foundOperationDrop {
		t.Errorf("expected span/operation sensitive-value diagnostics, got %v", plan.Diagnostics)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	rendered, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal rows: %v", err)
	}
	for _, canary := range []string{`alert(1)`, `";drop`, "topic", "group", "partition", "offset", "payload", "consumer_group"} {
		if strings.Contains(string(rendered), canary) {
			t.Errorf("rendered kafka output leaks %q", canary)
		}
	}
	for _, row := range rows {
		for _, panel := range row.Panels {
			for _, link := range panel.Links {
				if strings.Contains(link.URL, "var-operation=&") {
					t.Errorf("trace link carries an empty operation: %s", link.URL)
				}
			}
		}
	}
}

// TestKafkaPermutation requires identical rendered rows and diagnostics
// regardless of input order.
func TestKafkaPermutation(t *testing.T) {
	first := kafkaCatalog()
	second := kafkaCatalog()
	for left, right := 0, len(second.Items)-1; left < right; left, right = left+1, right-1 {
		second.Items[left], second.Items[right] = second.Items[right], second.Items[left]
	}
	firstPlan, err := BuildKafka(first, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka(first): %v", err)
	}
	secondPlan, err := BuildKafka(second, resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka(second): %v", err)
	}
	if !reflect.DeepEqual(firstPlan.Diagnostics, secondPlan.Diagnostics) {
		t.Errorf("diagnostics differ after permutation:\n%v\n%v", firstPlan.Diagnostics, secondPlan.Diagnostics)
	}
	firstRows, err := Render(firstPlan)
	if err != nil {
		t.Fatalf("Render(first): %v", err)
	}
	secondRows, err := Render(secondPlan)
	if err != nil {
		t.Fatalf("Render(second): %v", err)
	}
	if !reflect.DeepEqual(firstRows, secondRows) {
		t.Fatal("rendered kafka rows differ after permutation")
	}
}

// TestKafkaQueryLimit fails with DASHBOARD_PANEL_LIMIT_EXCEEDED instead of
// truncating when the row exceeds 150 queries. Each unique metric family
// contributes rate+error+3 percentiles+1 table target, so 13 producers and
// 13 consumers exceed the ceiling while panel count stays under 60.
func TestKafkaQueryLimit(t *testing.T) {
	items := make([]dashboard.DashboardItem, 0, 26)
	for index := range 13 {
		items = append(items, kafkaItem("dep:kafka:"+pad3(index), "kafka_producer", "op"+pad3(index),
			[]dashboard.SignalReference{
				metric("m"+pad3(index), "kafka_messages_"+pad3(index)+"_total", "counter", "service", "operation", "status"),
				metric("h"+pad3(index), "kafka_message_duration_"+pad3(index), "histogram", "service", "operation"),
			}, nil))
		items = append(items, kafkaItem("dep:kafka:c"+pad3(index), "kafka_consumer", "opc"+pad3(index),
			[]dashboard.SignalReference{
				metric("mc"+pad3(index), "kafka_consumer_"+pad3(index)+"_total", "counter", "service", "operation", "status"),
				metric("hc"+pad3(index), "kafka_consumer_duration_"+pad3(index), "histogram", "service", "operation"),
			}, nil))
	}
	catalog := &dashboard.DashboardCatalog{
		SchemaVersion: dashboard.CatalogSchemaVersion,
		ServiceName:   "payment",
		Items:         items,
	}
	plan, err := BuildKafka(catalog, resolvePolicy(t))
	if err == nil {
		t.Fatalf("BuildKafka must fail over the query ceiling, got %d queries", totalQueries(plan))
	}
	catalogError, ok := err.(*dashboard.CatalogError)
	if !ok || catalogError.Code != dashboard.CodePanelLimitExceeded {
		t.Fatalf("error = %v, want DASHBOARD_PANEL_LIMIT_EXCEEDED", err)
	}
}

func totalQueries(plan *Plan) int {
	total := 0
	for _, row := range plan.Rows {
		for _, panel := range row.Panels {
			total += len(panel.Targets)
		}
	}
	return total
}

func TestKafkaModelValidation(t *testing.T) {
	plan, err := BuildKafka(kafkaCatalog(), resolvePolicy(t))
	if err != nil {
		t.Fatalf("BuildKafka failed: %v", err)
	}
	rows, err := Render(plan)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	dashboard := &model.Dashboard{
		SchemaVersion: model.SchemaVersion,
		Title:         "payment Observability",
		UID:           "payment-uid",
		ID:            nil,
		Version:       0,
		Editable:      true,
		Timezone:      "browser",
		Refresh:       "30s",
		Templating: model.Templating{List: []model.Variable{{
			Name: "datasource", Type: model.VariableTypeDatasource, Query: "prometheus",
		}}},
		Rows:        rows,
		Annotations: model.Annotations{},
	}
	if violations := model.Validate(dashboard); len(violations) > 0 {
		t.Fatalf("kafka rows fail model validation: %v", violations)
	}
	if _, err := model.Render(dashboard); err != nil {
		t.Fatalf("kafka rows fail model render: %v", err)
	}
}

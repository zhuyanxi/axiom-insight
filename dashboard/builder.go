package dashboard

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// CatalogError is one fatal structural violation. Fatal errors abort the
// build; no partial catalog is returned.
type CatalogError struct {
	// Code is a stable DASHBOARD_* code.
	Code string
	// Field locates the issue, e.g. "metrics[2].target".
	Field string
	// ID identifies the offending plan or entity ID.
	ID string
	// Message explains the rule.
	Message string
}

// Error implements error.
func (failure *CatalogError) Error() string {
	location := failure.Field
	if failure.ID != "" {
		location = failure.Field + " " + failure.ID
	}
	return fmt.Sprintf("%s: %s: %s", failure.Code, location, failure.Message)
}

// CatalogErrors aggregates every fatal violation.
type CatalogErrors struct {
	violations []CatalogError
}

// Error implements error, one line per violation.
func (failures *CatalogErrors) Error() string {
	lines := make([]string, 0, len(failures.violations))
	for _, violation := range failures.violations {
		lines = append(lines, violation.Error())
	}
	return strings.Join(lines, "\n")
}

// Violations returns the individual violations.
func (failures *CatalogErrors) Violations() []CatalogError { return failures.violations }

// labelVocabulary is the controlled set of metric attribute keys that may
// become dashboard labels.
var labelVocabulary = map[string]bool{
	"service":   true,
	"operation": true,
	"status":    true,
}

// BuildCatalog maps the validated IR and its GenerationPlan onto a typed,
// traceable DashboardCatalog. The inputs are never modified. Fatal
// structural issues (nil inputs, unsupported schema, duplicate plan IDs,
// dangling or mismatched references, invalid labels) return a
// *CatalogErrors and no partial catalog; capability gaps and unmappable
// entities become diagnostics inside the catalog.
func BuildCatalog(document *observabilityv1.ObservabilityDocument, plan *observabilityv1.GenerationPlan, policy DashboardPolicy) (*DashboardCatalog, error) {
	var violations []CatalogError
	if document == nil {
		return nil, &CatalogErrors{violations: []CatalogError{{
			Code: CodeInvalidIR, Field: "$", Message: "document is nil",
		}}}
	}
	if plan == nil {
		return nil, &CatalogErrors{violations: []CatalogError{{
			Code: CodeInvalidIR, Field: "generation_plan", Message: "generation plan is nil",
		}}}
	}
	if document.GetSchemaVersion() != SupportedIRSchemaVersion {
		return nil, &CatalogErrors{violations: []CatalogError{{
			Code: CodeUnsupportedSchema, Field: "schema_version",
			Message: fmt.Sprintf("unsupported IR schema version %q", document.GetSchemaVersion()),
		}}}
	}
	if plan.GetSchemaVersion() != observabilityv1.GenerationPlanSchemaVersion {
		return nil, &CatalogErrors{violations: []CatalogError{{
			Code: CodeUnsupportedSchema, Field: "generation_plan.schema_version",
			Message: fmt.Sprintf("unsupported plan schema version %q", plan.GetSchemaVersion()),
		}}}
	}

	// Plan IDs must be unique across every signal before references can
	// be resolved.
	planIDs := make(map[string]bool, len(plan.GetMetrics())+len(plan.GetSpans())+len(plan.GetLogs()))
	for _, metric := range plan.GetMetrics() {
		if planIDs[metric.GetId()] {
			violations = append(violations, CatalogError{
				Code: CodeInvalidIR, Field: "metrics[].id", ID: metric.GetId(),
				Message: "duplicate plan ID",
			})
		}
		planIDs[metric.GetId()] = true
	}
	for _, span := range plan.GetSpans() {
		if planIDs[span.GetId()] {
			violations = append(violations, CatalogError{
				Code: CodeInvalidIR, Field: "spans[].id", ID: span.GetId(),
				Message: "duplicate plan ID",
			})
		}
		planIDs[span.GetId()] = true
	}
	for _, logPlan := range plan.GetLogs() {
		if planIDs[logPlan.GetId()] {
			violations = append(violations, CatalogError{
				Code: CodeInvalidIR, Field: "logs[].id", ID: logPlan.GetId(),
				Message: "duplicate plan ID",
			})
		}
		planIDs[logPlan.GetId()] = true
	}
	if len(violations) > 0 {
		return nil, &CatalogErrors{violations: violations}
	}

	catalog := &DashboardCatalog{
		SchemaVersion:               CatalogSchemaVersion,
		SourceIRSchemaVersion:       document.GetSchemaVersion(),
		GenerationPlanSchemaVersion: plan.GetSchemaVersion(),
		ServiceName:                 document.GetService().GetName(),
	}

	metricIndex := indexMetrics(document, plan, &violations)
	spanIndex := indexSpans(document, plan, &violations)
	if len(violations) > 0 {
		return nil, &CatalogErrors{violations: violations}
	}

	for index, endpoint := range document.GetEndpoints() {
		category, ok := endpointCategory(endpoint.GetKind())
		if !ok {
			catalog.Diagnostics = append(catalog.Diagnostics, Diagnostic{
				Code: CodeUnsupportedTarget, TargetID: endpoint.GetId(),
				Field:   "endpoints[" + endpoint.GetId() + "].kind",
				Message: "endpoint kind has no safe v1 dashboard mapping",
			})
			continue
		}
		item := buildEndpointItem(endpoint, index, category, metricIndex, spanIndex)
		catalog.Items = append(catalog.Items, item)
	}

	for index, dependency := range document.GetDependencies() {
		category, ok := dependencyCategory(dependency.GetKind())
		if !ok {
			catalog.Diagnostics = append(catalog.Diagnostics, Diagnostic{
				Code: CodeUnsupportedTarget, TargetID: dependency.GetId(),
				Field:   "dependencies[" + dependency.GetId() + "].kind",
				Message: "dependency kind has no safe v1 dashboard mapping",
			})
			continue
		}
		if isClientDependency(dependency.GetKind()) && !policy.IncludeClientDependencies {
			continue
		}
		item := buildDependencyItem(dependency, index, category, metricIndex, spanIndex)
		catalog.Items = append(catalog.Items, item)
	}

	sortCatalog(catalog)
	if policy.Strict {
		for _, diagnostic := range catalog.Diagnostics {
			if isWarning(diagnostic.Code) {
				return nil, &CatalogErrors{violations: []CatalogError{{
					Code: diagnostic.Code, Field: diagnostic.Field, ID: diagnostic.TargetID,
					Message: diagnostic.Message,
				}}}
			}
		}
	}
	return catalog, nil
}

// indexMetrics maps every metric plan onto its IR entity, verifying the
// target kind and ID match.
func indexMetrics(document *observabilityv1.ObservabilityDocument, plan *observabilityv1.GenerationPlan, violations *[]CatalogError) map[string][]*observabilityv1.MetricPlan {
	index := make(map[string][]*observabilityv1.MetricPlan)
	endpoints := make(map[string]bool, len(document.GetEndpoints()))
	for _, endpoint := range document.GetEndpoints() {
		endpoints[endpoint.GetId()] = true
	}
	dependencies := make(map[string]bool, len(document.GetDependencies()))
	for _, dependency := range document.GetDependencies() {
		dependencies[dependency.GetId()] = true
	}
	for metricIndex, metric := range plan.GetMetrics() {
		target := metric.GetTarget()
		field := "metrics[" + itoa(metricIndex) + "].target"
		if target == nil {
			*violations = append(*violations, CatalogError{
				Code: CodeInvalidIR, Field: field, ID: metric.GetId(), Message: "metric has no target",
			})
			continue
		}
		var exists bool
		switch target.GetKind() {
		case observabilityv1.TargetKind_TARGET_KIND_ENDPOINT:
			exists = endpoints[target.GetId()]
		case observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY:
			exists = dependencies[target.GetId()]
		}
		if !exists {
			*violations = append(*violations, CatalogError{
				Code: CodeDanglingReference, Field: field, ID: metric.GetId(),
				Message: fmt.Sprintf("references missing or mismatched entity %q", target.GetId()),
			})
			continue
		}
		index[target.GetId()] = append(index[target.GetId()], metric)
	}
	return index
}

// indexSpans maps every span plan onto its IR entity.
func indexSpans(document *observabilityv1.ObservabilityDocument, plan *observabilityv1.GenerationPlan, violations *[]CatalogError) map[string][]*observabilityv1.SpanPlan {
	index := make(map[string][]*observabilityv1.SpanPlan)
	endpoints := make(map[string]bool, len(document.GetEndpoints()))
	for _, endpoint := range document.GetEndpoints() {
		endpoints[endpoint.GetId()] = true
	}
	dependencies := make(map[string]bool, len(document.GetDependencies()))
	for _, dependency := range document.GetDependencies() {
		dependencies[dependency.GetId()] = true
	}
	for spanIndex, span := range plan.GetSpans() {
		target := span.GetTarget()
		field := "spans[" + itoa(spanIndex) + "].target"
		if target == nil {
			*violations = append(*violations, CatalogError{
				Code: CodeInvalidIR, Field: field, ID: span.GetId(), Message: "span has no target",
			})
			continue
		}
		var exists bool
		switch target.GetKind() {
		case observabilityv1.TargetKind_TARGET_KIND_ENDPOINT:
			exists = endpoints[target.GetId()]
		case observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY:
			exists = dependencies[target.GetId()]
		}
		if !exists {
			*violations = append(*violations, CatalogError{
				Code: CodeDanglingReference, Field: field, ID: span.GetId(),
				Message: fmt.Sprintf("references missing or mismatched entity %q", target.GetId()),
			})
			continue
		}
		index[target.GetId()] = append(index[target.GetId()], span)
	}
	return index
}

func buildEndpointItem(endpoint *observabilityv1.Endpoint, _ int, category Category, metricIndex map[string][]*observabilityv1.MetricPlan, spanIndex map[string][]*observabilityv1.SpanPlan) DashboardItem {
	operation := endpointOperation(endpoint)
	item := DashboardItem{
		ID:          stableItemID(category, endpoint.GetId()),
		Category:    category,
		Target:      TargetRef{Kind: "endpoint", ID: endpoint.GetId()},
		FunctionID:  endpoint.GetFunctionId(),
		DisplayName: safeDisplayName(endpoint.GetName(), category, operation),
		Operation:   operation,
		Provenance:  []string{"endpoints[" + endpoint.GetId() + "]"},
	}
	item.Metrics = metricReferences(metricIndex[endpoint.GetId()])
	item.Spans = spanReferences(spanIndex[endpoint.GetId()])
	item.Capabilities = capabilities(item.Metrics, item.Spans)
	return item
}

func buildDependencyItem(dependency *observabilityv1.Dependency, _ int, category Category, metricIndex map[string][]*observabilityv1.MetricPlan, spanIndex map[string][]*observabilityv1.SpanPlan) DashboardItem {
	operation := dependencyOperation(dependency)
	item := DashboardItem{
		ID:             stableItemID(category, dependency.GetId()),
		Category:       category,
		DependencyKind: dependencyKindName(dependency.GetKind()),
		Target:         TargetRef{Kind: "dependency", ID: dependency.GetId()},
		FunctionID:     dependency.GetFunctionId(),
		DisplayName:    safeDisplayName(dependency.GetName(), category, operation),
		Operation:      operation,
		Provenance:     []string{"dependencies[" + dependency.GetId() + "]"},
	}
	item.Metrics = metricReferences(metricIndex[dependency.GetId()])
	item.Spans = spanReferences(spanIndex[dependency.GetId()])
	item.Capabilities = capabilities(item.Metrics, item.Spans)
	return item
}

// dependencyKindName maps a Phase 1 dependency kind onto its stable
// catalog name. Producer/consumer classes stay distinct for P2-08.
func dependencyKindName(kind observabilityv1.DependencyKind) string {
	switch kind {
	case observabilityv1.DependencyKind_KAFKA_PRODUCER:
		return "kafka_producer"
	case observabilityv1.DependencyKind_KAFKA_CONSUMER:
		return "kafka_consumer"
	case observabilityv1.DependencyKind_SQL:
		return "sql"
	case observabilityv1.DependencyKind_REDIS:
		return "redis"
	case observabilityv1.DependencyKind_HTTP_CLIENT:
		return "http_client"
	case observabilityv1.DependencyKind_RPC_CLIENT:
		return "rpc_client"
	default:
		return ""
	}
}

// endpointCategory maps every endpoint kind onto a dashboard category.
// The mapping is exhaustive; unknown kinds are rejected, never guessed.
func endpointCategory(kind observabilityv1.EndpointKind) (Category, bool) {
	switch kind {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		return CategoryHTTP, true
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		return CategoryRPC, true
	case observabilityv1.EndpointKind_CRON_JOB:
		return CategoryServiceOverview, true
	default:
		return "", false
	}
}

// dependencyCategory maps every dependency kind onto a dashboard
// category.
func dependencyCategory(kind observabilityv1.DependencyKind) (Category, bool) {
	switch kind {
	case observabilityv1.DependencyKind_KAFKA_PRODUCER, observabilityv1.DependencyKind_KAFKA_CONSUMER:
		return CategoryKafka, true
	case observabilityv1.DependencyKind_SQL:
		return CategoryDatabase, true
	case observabilityv1.DependencyKind_REDIS:
		return CategoryCache, true
	case observabilityv1.DependencyKind_HTTP_CLIENT:
		return CategoryHTTP, true
	case observabilityv1.DependencyKind_RPC_CLIENT:
		return CategoryRPC, true
	default:
		return "", false
	}
}

func isClientDependency(kind observabilityv1.DependencyKind) bool {
	return kind == observabilityv1.DependencyKind_HTTP_CLIENT || kind == observabilityv1.DependencyKind_RPC_CLIENT
}

// metricReferences converts metric plans into traceable references with
// the controlled label vocabulary.
func metricReferences(metrics []*observabilityv1.MetricPlan) []SignalReference {
	references := make([]SignalReference, 0, len(metrics))
	for _, metric := range metrics {
		attributes := make([]string, 0, len(metric.GetAttributes()))
		for _, attribute := range metric.GetAttributes() {
			if labelVocabulary[attribute.GetKey()] {
				attributes = append(attributes, attribute.GetKey())
			}
		}
		sort.Strings(attributes)
		references = append(references, SignalReference{
			PlanID:     metric.GetId(),
			Name:       metric.GetName(),
			Type:       metricTypeName(metric.GetType()),
			Unit:       metric.GetUnit(),
			Attributes: attributes,
		})
	}
	return references
}

func spanReferences(spans []*observabilityv1.SpanPlan) []SignalReference {
	references := make([]SignalReference, 0, len(spans))
	for _, span := range spans {
		references = append(references, SignalReference{
			PlanID: span.GetId(),
			Name:   span.GetName(),
			Type:   spanKindName(span.GetKind()),
		})
	}
	return references
}

// capabilities gates each query class on the plan items present for the
// target.
func capabilities(metrics []SignalReference, spans []SignalReference) Capabilities {
	var counters, histograms, gauges int
	hasStatus := false
	hasServiceOperation := false
	for _, metric := range metrics {
		switch metric.Type {
		case "counter":
			counters++
		case "histogram":
			histograms++
		case "gauge":
			gauges++
		}
		if slices.Contains(metric.Attributes, "status") {
			hasStatus = true
		}
		if slices.Contains(metric.Attributes, "service") && slices.Contains(metric.Attributes, "operation") {
			hasServiceOperation = true
		}
	}
	return Capabilities{
		Rate: capability(hasServiceOperation,
			"no counter metric declares both service and operation attributes"),
		ErrorRatio: capability(hasStatus,
			"no metric declares the status attribute"),
		Percentiles: capability(histograms > 0,
			"no histogram metric is declared for this target"),
		InFlight: capability(gauges > 0,
			"no gauge metric is declared for this target"),
		TraceLink: capability(len(spans) > 0,
			"no span plan is declared for this target"),
	}
}

// endpointOperation derives the controlled operation per endpoint kind.
func endpointOperation(endpoint *observabilityv1.Endpoint) string {
	switch endpoint.GetKind() {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		operation, err := naming.NormalizeMachineName(endpoint.GetHttpMethod())
		if err == nil && operation != "" {
			return operation
		}
		return "http"
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		if endpoint.GetGrpcService() != "" && endpoint.GetGrpcMethod() != "" {
			operation, err := naming.NormalizeMachineName(endpoint.GetGrpcService() + "/" + endpoint.GetGrpcMethod())
			if err == nil {
				return operation
			}
		}
		return "grpc"
	default:
		operation, err := naming.NormalizeMachineName(endpoint.GetName())
		if err == nil && operation != "" {
			return operation
		}
		return "cron"
	}
}

// dependencyOperation derives the controlled dependency operation.
func dependencyOperation(dependency *observabilityv1.Dependency) string {
	operation, err := naming.NormalizeMachineName(dependency.GetOperation())
	if err != nil || operation == "" {
		return "unknown"
	}
	return operation
}

// stableItemID derives the deterministic item ID.
func stableItemID(category Category, targetID string) string {
	return "item:" + string(category) + ":" + targetID
}

// safeDisplayName builds a normalized display name; raw target values
// never enter it.
func safeDisplayName(entityName string, category Category, operation string) string {
	normalized, err := naming.NormalizeMachineName(entityName)
	if err != nil || normalized == "" {
		normalized = string(category)
	}
	return normalized + " " + operation
}

// sortCatalog orders items, references and diagnostics deterministically.
func sortCatalog(catalog *DashboardCatalog) {
	sort.Slice(catalog.Items, func(left, right int) bool {
		if catalog.Items[left].Category != catalog.Items[right].Category {
			return catalog.Items[left].Category < catalog.Items[right].Category
		}
		if catalog.Items[left].Target.Kind != catalog.Items[right].Target.Kind {
			return catalog.Items[left].Target.Kind < catalog.Items[right].Target.Kind
		}
		return catalog.Items[left].Target.ID < catalog.Items[right].Target.ID
	})
	for itemIndex := range catalog.Items {
		item := &catalog.Items[itemIndex]
		sort.Slice(item.Metrics, func(left, right int) bool {
			return item.Metrics[left].PlanID < item.Metrics[right].PlanID
		})
		sort.Slice(item.Spans, func(left, right int) bool {
			return item.Spans[left].PlanID < item.Spans[right].PlanID
		})
	}
	sort.Slice(catalog.Diagnostics, func(left, right int) bool {
		if catalog.Diagnostics[left].Code != catalog.Diagnostics[right].Code {
			return catalog.Diagnostics[left].Code < catalog.Diagnostics[right].Code
		}
		if catalog.Diagnostics[left].TargetID != catalog.Diagnostics[right].TargetID {
			return catalog.Diagnostics[left].TargetID < catalog.Diagnostics[right].TargetID
		}
		return catalog.Diagnostics[left].Field < catalog.Diagnostics[right].Field
	})
}

// isWarning reports whether a code is a strict-mode promotable warning.
func isWarning(code string) bool {
	switch code {
	case CodeMissingRequiredMetric, CodeUnsupportedTarget, CodeNameCollision, CodeSensitiveValueDropped:
		return true
	}
	return false
}

func metricTypeName(metricType observabilityv1.MetricType) string {
	switch metricType {
	case observabilityv1.MetricType_METRIC_TYPE_COUNTER:
		return "counter"
	case observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM:
		return "histogram"
	case observabilityv1.MetricType_METRIC_TYPE_GAUGE:
		return "gauge"
	case observabilityv1.MetricType_METRIC_TYPE_SUMMARY:
		return "summary"
	default:
		return "unspecified"
	}
}

func spanKindName(kind observabilityv1.SpanKind) string {
	switch kind {
	case observabilityv1.SpanKind_SPAN_KIND_SERVER:
		return "server"
	case observabilityv1.SpanKind_SPAN_KIND_CLIENT:
		return "client"
	case observabilityv1.SpanKind_SPAN_KIND_PRODUCER:
		return "producer"
	case observabilityv1.SpanKind_SPAN_KIND_CONSUMER:
		return "consumer"
	case observabilityv1.SpanKind_SPAN_KIND_INTERNAL:
		return "internal"
	default:
		return "unspecified"
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}

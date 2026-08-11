package dashboard

// Catalog schema versions. The catalog is versioned independently from
// the IR and the GenerationPlan.
const (
	// CatalogSchemaVersion is the DashboardCatalog contract version.
	CatalogSchemaVersion = "dashboard.catalog/v1"
	// SupportedIRSchemaVersion is the only accepted IR schema version.
	SupportedIRSchemaVersion = "v1"
)

// Category is one of the five logical dashboard areas plus the service
// overview bucket.
type Category string

// Categories.
const (
	CategoryServiceOverview Category = "service_overview"
	CategoryHTTP            Category = "http"
	CategoryRPC             Category = "rpc"
	CategoryKafka           Category = "kafka"
	CategoryDatabase        Category = "database"
	CategoryCache           Category = "cache"
)

// DashboardCatalog is the typed, fully traceable mapping of one service's
// IR and GenerationPlan onto dashboard items. Items and diagnostics are
// deterministically sorted.
type DashboardCatalog struct {
	// SchemaVersion is the catalog contract version.
	SchemaVersion string
	// SourceIRSchemaVersion mirrors the input IR version.
	SourceIRSchemaVersion string
	// GenerationPlanSchemaVersion mirrors the input Plan version.
	GenerationPlanSchemaVersion string
	// ServiceName matches the IR Service.name.
	ServiceName string
	// Items are the stable, sorted dashboard items.
	Items []DashboardItem
	// Diagnostics are the sorted catalog diagnostics; they never carry
	// rejected values.
	Diagnostics []Diagnostic
}

// DashboardItem is one entity mapped onto a dashboard category with its
// signal references and panel capabilities.
type DashboardItem struct {
	// ID is a stable item ID derived from the category and target ID:
	// "item:<category>:<target-id>".
	ID string
	// Category is the logical dashboard area.
	Category Category
	// Target references the IR entity this item is built from.
	Target TargetRef
	// FunctionID is the owning function's stable ID.
	FunctionID string
	// DisplayName is a safe, normalized display name.
	DisplayName string
	// Operation is the controlled, normalized operation.
	Operation string
	// Metrics are the metric plans declared for this target.
	Metrics []SignalReference
	// Spans are the span plans declared for this target.
	Spans []SignalReference
	// Capabilities gate which panels the Query Planner may generate.
	Capabilities Capabilities
	// Provenance records the stable input paths, e.g.
	// "endpoints[2]" or "dependencies[3]".
	Provenance []string
}

// TargetRef references an IR entity.
type TargetRef struct {
	// Kind is the IR entity kind.
	Kind string
	// ID is the stable IR entity ID.
	ID string
}

// SignalReference traces one plan item to its declaration.
type SignalReference struct {
	// PlanID is the stable GenerationPlan item ID.
	PlanID string
	// Name is the plan-declared name (metric name or span name).
	Name string
	// Type is the plan item type (metric type or span kind).
	Type string
	// Unit is the metric unit; empty for spans.
	Unit string
	// Attributes are the controlled low-cardinality label keys declared
	// by the metric plan, sorted.
	Attributes []string
}

// Capabilities gate panel generation. Every capability carries an
// explainable reason when unavailable, so a missing panel is always
// distinguishable from an unmappable one.
type Capabilities struct {
	Rate        QueryCapability
	ErrorRatio  QueryCapability
	Percentiles QueryCapability
	InFlight    QueryCapability
	TraceLink   QueryCapability
}

// QueryCapability reports whether a query class is provable from the
// plan.
type QueryCapability struct {
	// Available is true only when every required plan item exists.
	Available bool
	// Reason explains unavailability; empty when available.
	Reason string
}

// Diagnostic is one stable, locatable catalog issue. Messages never
// contain rejected values.
type Diagnostic struct {
	// Code is a stable DASHBOARD_* code.
	Code string
	// TargetID identifies the affected entity (empty for catalog-level
	// issues).
	TargetID string
	// Field locates the issue inside the input, e.g. "metrics[2].target".
	Field string
	// Message explains the rule without echoing values.
	Message string
}

// Capability helpers used by the builder.
func capability(available bool, reason string) QueryCapability {
	return QueryCapability{Available: available, Reason: reason}
}

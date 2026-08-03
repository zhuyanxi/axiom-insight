package generator

// OTelDocument is the closed v1 contract of otel.yaml. It is an
// instrumentation plan, never an OpenTelemetry Collector configuration.
type OTelDocument struct {
	SchemaVersion              string      `yaml:"schema_version" json:"schema_version"`
	DocumentType               string      `yaml:"document_type" json:"document_type"`
	PlanKind                   string      `yaml:"plan_kind" json:"plan_kind"`
	SemanticConventionsVersion string      `yaml:"semantic_conventions_version" json:"semantic_conventions_version"`
	Source                     Source      `yaml:"source" json:"source"`
	GeneratedBy                GeneratedBy `yaml:"generated_by" json:"generated_by"`
	Resources                  []Attribute `yaml:"resources,omitempty" json:"resources,omitempty"`
	Spans                      []Span      `yaml:"spans,omitempty" json:"spans,omitempty"`
}

// Span defines one span to be produced by the Runtime.
type Span struct {
	ID         string      `yaml:"id" json:"id"`
	Name       string      `yaml:"name" json:"name"`
	Kind       string      `yaml:"kind" json:"kind"`
	Target     TargetRef   `yaml:"target" json:"target"`
	Lifecycle  Lifecycle   `yaml:"lifecycle" json:"lifecycle"`
	Parent     Parent      `yaml:"parent" json:"parent"`
	Attributes []Attribute `yaml:"attributes,omitempty" json:"attributes,omitempty"`
	// Status maps every runtime operation status to an OTel-compatible
	// status. When present it must cover all five statuses exactly.
	Status *StatusPolicy `yaml:"status,omitempty" json:"status,omitempty"`
	Events []SpanEvent   `yaml:"events,omitempty" json:"events,omitempty"`
}

// Lifecycle binds span start and end to runtime triggers.
type Lifecycle struct {
	Start string `yaml:"start" json:"start"`
	End   string `yaml:"end" json:"end"`
}

// Parent decides how a span's parent is established at runtime.
type Parent struct {
	Strategy string `yaml:"strategy" json:"strategy"`
	// Carrier is required for extract_or_root.
	Carrier string `yaml:"carrier,omitempty" json:"carrier,omitempty"`
	// StaticParentSpanID is required for static and must reference another
	// Span id in the same document.
	StaticParentSpanID string `yaml:"static_parent_span_id,omitempty" json:"static_parent_span_id,omitempty"`
}

// StatusPolicy maps each runtime status to a span status setting.
type StatusPolicy struct {
	Mapping map[string]string `yaml:"mapping" json:"mapping"`
}

// SpanEvent defines a controlled event emitted by a span. Raw error strings
// never become event names.
type SpanEvent struct {
	ID string `yaml:"id" json:"id"`
	// Name is a controlled static event name, e.g. "exception".
	Name string `yaml:"name" json:"name"`
	// Condition selects the event from a controlled set.
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
	// Statuses lists the runtime statuses that fire this event. Empty means
	// unconditional.
	Statuses   []string    `yaml:"statuses,omitempty" json:"statuses,omitempty"`
	Attributes []Attribute `yaml:"attributes,omitempty" json:"attributes,omitempty"`
}

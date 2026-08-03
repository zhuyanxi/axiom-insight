package generator

// LoggingDocument is the closed v1 contract of logging.yaml.
type LoggingDocument struct {
	SchemaVersion string      `yaml:"schema_version" json:"schema_version"`
	DocumentType  string      `yaml:"document_type" json:"document_type"`
	Source        Source      `yaml:"source" json:"source"`
	GeneratedBy   GeneratedBy `yaml:"generated_by" json:"generated_by"`
	Redaction     Redaction   `yaml:"redaction" json:"redaction"`
	Events        []LogEvent  `yaml:"events" json:"events"`
}

// Redaction declares the immutable built-in credential denylist plus any
// user additions. Immutable must stay true; the built-in rules cannot be
// relaxed.
type Redaction struct {
	Immutable  bool     `yaml:"immutable" json:"immutable"`
	FieldNames []string `yaml:"field_names" json:"field_names"`
}

// LogEvent defines one structured log event.
type LogEvent struct {
	ID        string    `yaml:"id" json:"id"`
	EventName string    `yaml:"event_name" json:"event_name"`
	Target    TargetRef `yaml:"target" json:"target"`
	Trigger   string    `yaml:"trigger" json:"trigger"`
	Condition Condition `yaml:"condition" json:"condition"`
	Severity  Severity  `yaml:"severity" json:"severity"`
	Fields    []Field   `yaml:"fields" json:"fields"`
}

// Condition selects which runtime statuses fire an event. It must be
// non-empty and mutually exclusive across events of the same target.
type Condition struct {
	StatusIn []string `yaml:"status_in" json:"status_in"`
}

// Severity is a static log severity.
type Severity struct {
	Constant string `yaml:"constant" json:"constant"`
}

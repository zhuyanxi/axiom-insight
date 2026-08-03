package generator

// Source is the common document header block describing the analyzed source.
type Source struct {
	// IRSchemaVersion records the ObservabilityDocument schema version the
	// document was derived from.
	IRSchemaVersion string `yaml:"ir_schema_version" json:"ir_schema_version"`
	// ServiceName matches the source IR Service.name and is constant across
	// a single document.
	ServiceName string `yaml:"service_name" json:"service_name"`
}

// GeneratedBy identifies the generator that produced the document.
type GeneratedBy struct {
	// Name is the generator name, e.g. "si".
	Name string `yaml:"name" json:"name"`
	// Version is the concrete generator version, e.g. "v0.2.0".
	Version string `yaml:"version" json:"version"`
}

// TargetRef is a strongly typed reference to an IR entity. The type must
// match the actual entity kind the referenced ID points to.
type TargetRef struct {
	Type string `yaml:"type" json:"type"`
	ID   string `yaml:"id" json:"id"`
}

// ValueBinding states where a bound value comes from and how the Runtime
// must treat it. Plan constants, IR constants and runtime sources stay
// distinct so no consumer has to guess provenance.
type ValueBinding struct {
	// Source classifies the value source.
	Source string `yaml:"source" json:"source"`
	// Path is the stable dotted source path, e.g. "runtime.operation.status"
	// or "endpoint.http_path". Required for IR and runtime sources.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// String holds a constant string value (source "constant").
	String string `yaml:"string,omitempty" json:"string,omitempty"`
	// Number holds a constant numeric value (source "constant").
	Number float64 `yaml:"number,omitempty" json:"number,omitempty"`
	// Bool holds a constant boolean value (source "constant").
	Bool *bool `yaml:"bool,omitempty" json:"bool,omitempty"`
	// AllowedValues constrains a runtime status binding to the finite status
	// vocabulary.
	AllowedValues []string `yaml:"allowed_values,omitempty" json:"allowed_values,omitempty"`
	// Fallback is a static fallback value used when the runtime source is
	// absent and the binding is not required. It never carries secrets.
	Fallback string `yaml:"fallback,omitempty" json:"fallback,omitempty"`
	// Required marks values the Runtime must provide. Absent means optional.
	Required *bool `yaml:"required,omitempty" json:"required,omitempty"`
	// Type is the data type of the bound value.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// Attribute binds one static metric, span or resource attribute key to a
// value.
type Attribute struct {
	Key     string       `yaml:"key" json:"key"`
	Type    string       `yaml:"type,omitempty" json:"type,omitempty"`
	Binding ValueBinding `yaml:"binding" json:"binding"`
}

// Field binds one structured log field key to a value.
type Field struct {
	Key      string       `yaml:"key" json:"key"`
	Type     string       `yaml:"type" json:"type"`
	Required *bool        `yaml:"required,omitempty" json:"required,omitempty"`
	Binding  ValueBinding `yaml:"binding" json:"binding"`
}

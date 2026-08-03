package generator

// MetricsDocument is the closed v1 contract of metrics.yaml.
type MetricsDocument struct {
	SchemaVersion string      `yaml:"schema_version" json:"schema_version"`
	DocumentType  string      `yaml:"document_type" json:"document_type"`
	Source        Source      `yaml:"source" json:"source"`
	GeneratedBy   GeneratedBy `yaml:"generated_by" json:"generated_by"`
	Metrics       []Metric    `yaml:"metrics" json:"metrics"`
}

// Metric defines one metric instrument and how to record it.
type Metric struct {
	ID          string    `yaml:"id" json:"id"`
	Name        string    `yaml:"name" json:"name"`
	Type        string    `yaml:"type" json:"type"`
	Unit        string    `yaml:"unit" json:"unit"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	Target      TargetRef `yaml:"target" json:"target"`
	Record      Record    `yaml:"record" json:"record"`
	// Buckets are the finite histogram bucket boundaries. Only valid for
	// histograms; strictly increasing and positive.
	Buckets []float64 `yaml:"buckets,omitempty" json:"buckets,omitempty"`
	// Quantiles are the summary quantiles. Only valid for summaries;
	// strictly increasing and within (0,1).
	Quantiles  []float64   `yaml:"quantiles,omitempty" json:"quantiles,omitempty"`
	Attributes []Attribute `yaml:"attributes,omitempty" json:"attributes,omitempty"`
}

// Record describes when the instrument is recorded and what value it takes.
type Record struct {
	Trigger string       `yaml:"trigger" json:"trigger"`
	Value   ValueBinding `yaml:"value" json:"value"`
}

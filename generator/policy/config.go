package policy

import "regexp"

// GenerationConfig is the user-supplied si.yaml `generation` node. Every
// field that a default could override is a pointer (or a nil slice) so the
// merge rules can distinguish "explicitly set" from "absent": an explicit
// false survives the default layer and an explicit CLI value survives the
// YAML layer. A nil pointer or nil slice always means "not set".
//
// Values are validated only after the full merge; Resolve is the only
// entry point that produces a valid, immutable Policy.
type GenerationConfig struct {
	// OutputDir is the output directory relative to the source root, or
	// an explicit absolute directory.
	OutputDir *string `json:"output_dir"`
	// Signals names the signals to generate: metrics, tracing, logging.
	// Duplicates are removed; the stored order is the fixed contract
	// order. An empty list is a configuration error.
	Signals []string `json:"signals"`
	// Strict promotes generator warnings to failures.
	Strict *bool `json:"strict"`

	Metrics *MetricsConfig `json:"metrics"`
	Tracing *TracingConfig `json:"tracing"`
	Logging *LoggingConfig `json:"logging"`
}

// MetricsConfig configures the metrics signal.
type MetricsConfig struct {
	// Namespace is an optional metrics name prefix; empty means none.
	Namespace string `json:"namespace"`
	// HistogramBucketsSeconds are finite, strictly increasing, positive
	// bucket boundaries; between 1 and MaxHistogramBuckets.
	HistogramBucketsSeconds []float64 `json:"histogram_buckets_seconds"`
	// IncludeInFlightGauges controls in-flight operation gauges.
	IncludeInFlightGauges *bool `json:"include_in_flight_gauges"`
	// MaxInstruments bounds generated instruments; positive and at most
	// HardMaxInstruments.
	MaxInstruments *int64 `json:"max_instruments"`
	// MaxEstimatedSeries bounds estimated time series; positive and at
	// most HardMaxEstimatedSeries.
	MaxEstimatedSeries *int64 `json:"max_estimated_series"`

	Summaries *SummariesConfig `json:"summaries"`
}

// SummariesConfig configures optional summary instruments. Quantiles are
// read and validated only when Enabled is true.
type SummariesConfig struct {
	// Enabled turns summary instruments on (default false).
	Enabled *bool `json:"enabled"`
	// Quantiles are summary quantiles in [0, 1], strictly increasing,
	// between 1 and MaxQuantiles entries. Ignored when Enabled is not
	// true.
	Quantiles []float64 `json:"quantiles"`
}

// TracingConfig configures the tracing signal. The OpenTelemetry Semantic
// Conventions version is fixed by the built-in constant and cannot be
// configured.
type TracingConfig struct {
	// IncludeInternalCalls generates INTERNAL child spans for resolved
	// internal call edges when enabled.
	IncludeInternalCalls *bool `json:"include_internal_calls"`
	// RecordExceptionEvents records exception events for error statuses.
	RecordExceptionEvents *bool `json:"record_exception_events"`
}

// LoggingConfig configures the logging signal.
type LoggingConfig struct {
	// EmitStartEvents emits operation start events.
	EmitStartEvents *bool `json:"emit_start_events"`
	// EmitCompletionEvents emits operation completion events.
	EmitCompletionEvents *bool `json:"emit_completion_events"`
	// EmitDependencyErrors emits dependency failure events.
	EmitDependencyErrors *bool `json:"emit_dependency_errors"`
	// CorrelationFields selects correlation fields from the allowlist.
	CorrelationFields []string `json:"correlation_fields"`
	// RedactFields names log fields that must be redacted. Entries are
	// normalized to lowercase and deduplicated; the built-in credential
	// denylist cannot be removed.
	RedactFields []string `json:"redact_fields"`
}

// Overrides carries explicit CLI flag values. Merge priority is fixed:
// CLI flags > si.yaml > built-in defaults. A nil pointer (or nil slice)
// means the flag was not set and the YAML/default value stands.
type Overrides struct {
	OutputDir *string `json:"output_dir"`
	Signals   []string `json:"signals"`
	Strict    *bool   `json:"strict"`

	Metrics MetricsOverrides `json:"metrics"`
	Tracing TracingOverrides `json:"tracing"`
	Logging LoggingOverrides `json:"logging"`
}

// MetricsOverrides are the CLI overrides for the metrics signal.
type MetricsOverrides struct {
	Namespace               *string         `json:"namespace"`
	HistogramBucketsSeconds []float64       `json:"histogram_buckets_seconds"`
	IncludeInFlightGauges   *bool           `json:"include_in_flight_gauges"`
	MaxInstruments          *int64          `json:"max_instruments"`
	MaxEstimatedSeries      *int64          `json:"max_estimated_series"`
	Summaries               SummariesOverrides `json:"summaries"`
}

// SummariesOverrides are the CLI overrides for summary instruments.
type SummariesOverrides struct {
	Enabled   *bool     `json:"enabled"`
	Quantiles []float64 `json:"quantiles"`
}

// TracingOverrides are the CLI overrides for the tracing signal.
type TracingOverrides struct {
	IncludeInternalCalls  *bool `json:"include_internal_calls"`
	RecordExceptionEvents *bool `json:"record_exception_events"`
}

// LoggingOverrides are the CLI overrides for the logging signal.
type LoggingOverrides struct {
	EmitStartEvents      *bool     `json:"emit_start_events"`
	EmitCompletionEvents *bool     `json:"emit_completion_events"`
	EmitDependencyErrors *bool     `json:"emit_dependency_errors"`
	CorrelationFields    []string  `json:"correlation_fields"`
	RedactFields         []string  `json:"redact_fields"`
}

// namespacePattern is the allowed metrics namespace charset: ASCII letter,
// digit, underscore or colon, starting with a letter, underscore or colon.
var namespacePattern = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

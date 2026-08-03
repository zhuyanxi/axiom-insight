package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Digest returns a deterministic fingerprint of the policy contents using
// canonical JSON (fixed struct field order) over a SHA-256 hash. The
// digest excludes OutputDir on purpose: the same semantic configuration
// must produce the same digest regardless of the working directory or
// output path. The digest never includes secrets; redaction lists contain
// field names only.
func (p *Policy) Digest() string {
	payload := digestPayload{
		Signals: p.Signals,
		Strict:  p.Strict,
		Metrics: metricsDigest{
			Namespace:               p.Metrics.Namespace,
			HistogramBucketsSeconds: p.Metrics.HistogramBucketsSeconds,
			IncludeInFlightGauges:   p.Metrics.IncludeInFlightGauges,
			MaxInstruments:          p.Metrics.MaxInstruments,
			MaxEstimatedSeries:      p.Metrics.MaxEstimatedSeries,
			Summaries: summariesDigest{
				Enabled:   p.Metrics.Summaries.Enabled,
				Quantiles: p.Metrics.Summaries.Quantiles,
			},
		},
		Tracing: tracingDigest{
			IncludeInternalCalls:     p.Tracing.IncludeInternalCalls,
			RecordExceptionEvents:    p.Tracing.RecordExceptionEvents,
			SemanticConventionsVersion: p.Tracing.SemanticConventionsVersion,
		},
		Logging: loggingDigest{
			EmitStartEvents:      p.Logging.EmitStartEvents,
			EmitCompletionEvents: p.Logging.EmitCompletionEvents,
			EmitDependencyErrors: p.Logging.EmitDependencyErrors,
			CorrelationFields:    p.Logging.CorrelationFields,
			RedactFields:         p.Logging.RedactFields,
		},
	}
	// Field order of the payload structs is the canonical JSON order;
	// encoding/json never reorders struct fields, so the bytes are stable.
	contents, err := json.Marshal(payload)
	if err != nil {
		// The payload contains only strings, bools and floats; marshaling
		// cannot fail for any policy this package builds.
		panic("policy digest marshal failed: " + err.Error())
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

type digestPayload struct {
	Signals []string      `json:"signals"`
	Strict  bool          `json:"strict"`
	Metrics metricsDigest `json:"metrics"`
	Tracing tracingDigest `json:"tracing"`
	Logging loggingDigest `json:"logging"`
}

type metricsDigest struct {
	Namespace               string          `json:"namespace"`
	HistogramBucketsSeconds []float64       `json:"histogram_buckets_seconds"`
	IncludeInFlightGauges   bool            `json:"include_in_flight_gauges"`
	MaxInstruments          int64           `json:"max_instruments"`
	MaxEstimatedSeries      int64           `json:"max_estimated_series"`
	Summaries               summariesDigest `json:"summaries"`
}

type summariesDigest struct {
	Enabled   bool      `json:"enabled"`
	Quantiles []float64 `json:"quantiles"`
}

type tracingDigest struct {
	IncludeInternalCalls     bool   `json:"include_internal_calls"`
	RecordExceptionEvents    bool   `json:"record_exception_events"`
	SemanticConventionsVersion string `json:"semantic_conventions_version"`
}

type loggingDigest struct {
	EmitStartEvents      bool     `json:"emit_start_events"`
	EmitCompletionEvents bool     `json:"emit_completion_events"`
	EmitDependencyErrors bool     `json:"emit_dependency_errors"`
	CorrelationFields    []string `json:"correlation_fields"`
	RedactFields         []string `json:"redact_fields"`
}

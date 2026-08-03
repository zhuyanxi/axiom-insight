package policy

// Built-in defaults for the si.yaml `generation` node. Values mirror the
// Phase 1 contract (section 4). Changing a default requires an explicit
// review of the default-policy snapshot test.
const (
	// DefaultOutputDir is the default `generate` output directory relative
	// to the source root.
	DefaultOutputDir = "generate"
	// DefaultMaxInstruments is the default upper bound on generated
	// instruments per plan.
	DefaultMaxInstruments = 10_000
	// DefaultMaxEstimatedSeries is the default upper bound on estimated
	// time series per plan.
	DefaultMaxEstimatedSeries = 100_000
	// HardMaxInstruments is the implementation safety ceiling for
	// max_instruments; user values above it are rejected.
	HardMaxInstruments = 1_000_000
	// HardMaxEstimatedSeries is the implementation safety ceiling for
	// max_estimated_series; user values above it are rejected.
	HardMaxEstimatedSeries = 10_000_000
	// MaxNamespaceLength bounds the metrics namespace name.
	MaxNamespaceLength = 64
	// MaxHistogramBuckets bounds the number of finite bucket boundaries.
	MaxHistogramBuckets = 50
	// MaxQuantiles bounds the number of summary quantiles.
	MaxQuantiles = 10
	// BuiltinSemanticConventionsVersion is the pinned OpenTelemetry
	// Semantic Conventions version for Phase 1. It is not configurable; a
	// YAML field with this name is rejected as unknown.
	BuiltinSemanticConventionsVersion = "1.37.0"
)

// DefaultHistogramBucketsSeconds is the default finite bucket boundaries
// for duration histograms, strictly increasing and finite.
var DefaultHistogramBucketsSeconds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// DefaultQuantiles is the default summary quantile set, strictly
// increasing and within [0, 1].
var DefaultQuantiles = []float64{0.5, 0.9, 0.99}

// Signal names and their fixed output order. Deduplicated and stored in
// exactly this order regardless of input order.
var signalOrder = []string{"metrics", "tracing", "logging"}

// signalAllowed indexes allowed signal names.
var signalAllowed = map[string]bool{
	"metrics": true,
	"tracing": true,
	"logging": true,
}

// DefaultSignals returns a copy of the default enabled signal list.
func DefaultSignals() []string { return append([]string(nil), signalOrder...) }

// DefaultCorrelationFields is the allowlist of log correlation fields.
var DefaultCorrelationFields = []string{"request_id", "trace_id", "span_id"}

// correlationFieldAllowed indexes the correlation field allowlist. Only
// these names may appear in logging.correlation_fields.
var correlationFieldAllowed = map[string]bool{
	"request_id": true,
	"trace_id":   true,
	"span_id":    true,
}

// DefaultRedactFields is the default log redaction field-name list.
var DefaultRedactFields = []string{"authorization", "cookie", "password", "secret", "token"}

// BuiltinCredentialDenylist is the credential denylist that cannot be
// disabled: a user redact_fields list that omits any of these entries is
// rejected with GEN_INVALID_CONFIG. Users may add further names.
var BuiltinCredentialDenylist = map[string]bool{
	"authorization": true,
	"cookie":        true,
	"password":      true,
	"secret":        true,
	"token":         true,
}

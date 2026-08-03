package policy

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// Policy is the fully resolved, validated generation configuration. It is
// immutable after Resolve returns: callers must not mutate exported
// slices. All slice values are fresh copies; they are never shared with
// the input configuration.
type Policy struct {
	// OutputDir is the resolved output directory.
	OutputDir string
	// Signals lists enabled signals in the fixed contract order.
	Signals []string
	// Strict promotes generator warnings to failures.
	Strict bool

	Metrics MetricsPolicy
	Tracing TracingPolicy
	Logging LoggingPolicy
}

// MetricsPolicy is the resolved metrics configuration.
type MetricsPolicy struct {
	// Namespace is an optional metric name prefix; empty means none.
	Namespace string
	// HistogramBucketsSeconds are the finite bucket boundaries, strictly
	// increasing and positive.
	HistogramBucketsSeconds []float64
	// IncludeInFlightGauges controls in-flight operation gauges.
	IncludeInFlightGauges bool
	// MaxInstruments bounds generated instruments.
	MaxInstruments int64
	// MaxEstimatedSeries bounds estimated time series.
	MaxEstimatedSeries int64
	// Summaries configures optional summary instruments.
	Summaries SummariesPolicy
}

// SummariesPolicy is the resolved summary configuration.
type SummariesPolicy struct {
	// Enabled turns summary instruments on.
	Enabled bool
	// Quantiles are the validated summary quantiles; only meaningful
	// when Enabled is true.
	Quantiles []float64
}

// TracingPolicy is the resolved tracing configuration.
type TracingPolicy struct {
	// IncludeInternalCalls generates INTERNAL child spans for resolved
	// internal call edges.
	IncludeInternalCalls bool
	// RecordExceptionEvents records exception events for error statuses.
	RecordExceptionEvents bool
	// SemanticConventionsVersion is the pinned OpenTelemetry Semantic
	// Conventions version; it is not user-configurable.
	SemanticConventionsVersion string
}

// LoggingPolicy is the resolved logging configuration.
type LoggingPolicy struct {
	// EmitStartEvents emits operation start events.
	EmitStartEvents bool
	// EmitCompletionEvents emits operation completion events.
	EmitCompletionEvents bool
	// EmitDependencyErrors emits dependency failure events.
	EmitDependencyErrors bool
	// CorrelationFields are the validated correlation field names.
	CorrelationFields []string
	// RedactFields are the normalized, deduplicated, sorted redaction
	// field names; always a superset of the built-in credential denylist.
	RedactFields []string
}

// ConfigError describes one configuration violation. Field is a dotted
// path into the generation node, for example
// "generation.metrics.histogram_buckets_seconds[2]".
type ConfigError struct {
	// Field is the configuration path of the offending value.
	Field string
	// Message explains the violated rule without echoing configuration
	// values that could carry secrets.
	Message string
}

// ConfigErrors aggregates every violation found while resolving a
// configuration. It implements error and prefixes each line with the
// stable GEN_INVALID_CONFIG message code.
type ConfigErrors struct {
	violations []ConfigError
}

// Error implements error. It never includes the full configuration
// contents, only field paths and rule messages.
func (failures *ConfigErrors) Error() string {
	var builder strings.Builder
	for index, violation := range failures.violations {
		if index > 0 {
			builder.WriteByte('\n')
		}
		fmt.Fprintf(&builder, "%s: %s: %s", CodeInvalidConfig, violation.Field, violation.Message)
	}
	return builder.String()
}

// Violations returns the individual violations. The returned slice must
// not be modified.
func (failures *ConfigErrors) Violations() []ConfigError { return failures.violations }

// Resolve merges defaults, the YAML configuration and the CLI overrides
// (CLI > YAML > defaults), normalizes and validates the result and returns
// the immutable Policy. A nil config and nil overrides produce the
// built-in default policy. On any violation it returns nil and a
// *ConfigErrors whose paths locate every offending field.
func Resolve(config *GenerationConfig, overrides *Overrides) (*Policy, error) {
	effective := newEffective()

	// Layer 1: built-in defaults.
	applyDefaults(effective)
	// Layer 2: explicit si.yaml values.
	applyConfig(effective, config)
	// Layer 3: explicit CLI flags.
	applyOverrides(effective, overrides)

	if violations := validateEffective(effective); len(violations) > 0 {
		return nil, &ConfigErrors{violations: violations}
	}
	return effective.build(), nil
}

// effective is the merged working configuration: every pointer is
// non-nil before validation runs.
type effective struct {
	outputDir string
	signals   []string
	strict    bool

	namespace              string
	buckets                []float64
	includeInFlightGauges  bool
	maxInstruments         int64
	maxEstimatedSeries     int64
	summariesEnabled       bool
	quantiles              []float64

	includeInternalCalls   bool
	recordExceptionEvents  bool

	emitStartEvents        bool
	emitCompletionEvents   bool
	emitDependencyErrors   bool
	correlationFields      []string
	redactFields           []string
}

func newEffective() *effective { return &effective{} }

func applyDefaults(target *effective) {
	target.outputDir = DefaultOutputDir
	target.signals = DefaultSignals()
	target.strict = false

	target.namespace = ""
	target.buckets = append([]float64(nil), DefaultHistogramBucketsSeconds...)
	target.includeInFlightGauges = true
	target.maxInstruments = DefaultMaxInstruments
	target.maxEstimatedSeries = DefaultMaxEstimatedSeries
	target.summariesEnabled = false
	target.quantiles = append([]float64(nil), DefaultQuantiles...)

	target.includeInternalCalls = false
	target.recordExceptionEvents = true

	target.emitStartEvents = false
	target.emitCompletionEvents = true
	target.emitDependencyErrors = true
	target.correlationFields = append([]string(nil), DefaultCorrelationFields...)
	target.redactFields = append([]string(nil), DefaultRedactFields...)
}

func applyConfig(target *effective, config *GenerationConfig) {
	if config == nil {
		return
	}
	if config.OutputDir != nil {
		target.outputDir = *config.OutputDir
	}
	if config.Signals != nil {
		target.signals = append([]string(nil), config.Signals...)
	}
	if config.Strict != nil {
		target.strict = *config.Strict
	}
	if metrics := config.Metrics; metrics != nil {
		if metrics.Namespace != "" {
			target.namespace = metrics.Namespace
		}
		if metrics.HistogramBucketsSeconds != nil {
			target.buckets = append([]float64(nil), metrics.HistogramBucketsSeconds...)
		}
		if metrics.IncludeInFlightGauges != nil {
			target.includeInFlightGauges = *metrics.IncludeInFlightGauges
		}
		if metrics.MaxInstruments != nil {
			target.maxInstruments = *metrics.MaxInstruments
		}
		if metrics.MaxEstimatedSeries != nil {
			target.maxEstimatedSeries = *metrics.MaxEstimatedSeries
		}
		if summaries := metrics.Summaries; summaries != nil {
			if summaries.Enabled != nil {
				target.summariesEnabled = *summaries.Enabled
			}
			// Quantiles are read only when summaries are enabled; an
			// explicit quantile list with summaries disabled is ignored.
			if summaries.Enabled != nil && *summaries.Enabled && summaries.Quantiles != nil {
				target.quantiles = append([]float64(nil), summaries.Quantiles...)
			}
		}
	}
	if tracing := config.Tracing; tracing != nil {
		if tracing.IncludeInternalCalls != nil {
			target.includeInternalCalls = *tracing.IncludeInternalCalls
		}
		if tracing.RecordExceptionEvents != nil {
			target.recordExceptionEvents = *tracing.RecordExceptionEvents
		}
	}
	if logging := config.Logging; logging != nil {
		if logging.EmitStartEvents != nil {
			target.emitStartEvents = *logging.EmitStartEvents
		}
		if logging.EmitCompletionEvents != nil {
			target.emitCompletionEvents = *logging.EmitCompletionEvents
		}
		if logging.EmitDependencyErrors != nil {
			target.emitDependencyErrors = *logging.EmitDependencyErrors
		}
		if logging.CorrelationFields != nil {
			target.correlationFields = append([]string(nil), logging.CorrelationFields...)
		}
		if logging.RedactFields != nil {
			target.redactFields = append([]string(nil), logging.RedactFields...)
		}
	}
}

func applyOverrides(target *effective, overrides *Overrides) {
	if overrides == nil {
		return
	}
	if overrides.OutputDir != nil {
		target.outputDir = *overrides.OutputDir
	}
	if overrides.Signals != nil {
		target.signals = append([]string(nil), overrides.Signals...)
	}
	if overrides.Strict != nil {
		target.strict = *overrides.Strict
	}
	metrics := &overrides.Metrics
	if metrics.Namespace != nil {
		target.namespace = *metrics.Namespace
	}
	if metrics.HistogramBucketsSeconds != nil {
		target.buckets = append([]float64(nil), metrics.HistogramBucketsSeconds...)
	}
	if metrics.IncludeInFlightGauges != nil {
		target.includeInFlightGauges = *metrics.IncludeInFlightGauges
	}
	if metrics.MaxInstruments != nil {
		target.maxInstruments = *metrics.MaxInstruments
	}
	if metrics.MaxEstimatedSeries != nil {
		target.maxEstimatedSeries = *metrics.MaxEstimatedSeries
	}
	if metrics.Summaries.Enabled != nil {
		target.summariesEnabled = *metrics.Summaries.Enabled
	}
	if metrics.Summaries.Quantiles != nil {
		target.quantiles = append([]float64(nil), metrics.Summaries.Quantiles...)
	}
	if overrides.Tracing.IncludeInternalCalls != nil {
		target.includeInternalCalls = *overrides.Tracing.IncludeInternalCalls
	}
	if overrides.Tracing.RecordExceptionEvents != nil {
		target.recordExceptionEvents = *overrides.Tracing.RecordExceptionEvents
	}
	if overrides.Logging.EmitStartEvents != nil {
		target.emitStartEvents = *overrides.Logging.EmitStartEvents
	}
	if overrides.Logging.EmitCompletionEvents != nil {
		target.emitCompletionEvents = *overrides.Logging.EmitCompletionEvents
	}
	if overrides.Logging.EmitDependencyErrors != nil {
		target.emitDependencyErrors = *overrides.Logging.EmitDependencyErrors
	}
	if overrides.Logging.CorrelationFields != nil {
		target.correlationFields = append([]string(nil), overrides.Logging.CorrelationFields...)
	}
	if overrides.Logging.RedactFields != nil {
		target.redactFields = append([]string(nil), overrides.Logging.RedactFields...)
	}
}

// validateEffective normalizes first, then checks every rule. Violations
// carry exact field paths such as
// "generation.metrics.histogram_buckets_seconds[2]".
func validateEffective(target *effective) []ConfigError {
	normalize(target)
	var violations []ConfigError

	if strings.TrimSpace(target.outputDir) == "" {
		violations = append(violations, ConfigError{
			Field:   "generation.output_dir",
			Message: "output_dir must not be empty",
		})
	}
	if strings.ContainsRune(target.outputDir, 0) {
		violations = append(violations, ConfigError{
			Field:   "generation.output_dir",
			Message: "output_dir must not contain NUL",
		})
	}

	if len(target.signals) == 0 {
		violations = append(violations, ConfigError{
			Field:   "generation.signals",
			Message: "at least one signal is required; use [metrics, tracing, logging]",
		})
	}
	for index, signal := range target.signals {
		if !signalAllowed[signal] {
			violations = append(violations, ConfigError{
				Field:   fmt.Sprintf("generation.signals[%d]", index),
				Message: fmt.Sprintf("unsupported signal %q; allowed: metrics, tracing, logging", signal),
			})
		}
	}

	violations = append(violations, validateNamespace(target.namespace)...)
	violations = append(violations, validateBuckets(target.buckets)...)
	violations = append(violations, validateLimits("generation.metrics.max_instruments", target.maxInstruments, HardMaxInstruments)...)
	violations = append(violations, validateLimits("generation.metrics.max_estimated_series", target.maxEstimatedSeries, HardMaxEstimatedSeries)...)
	if target.summariesEnabled {
		violations = append(violations, validateQuantiles(target.quantiles)...)
	}

	for index, field := range target.correlationFields {
		if !correlationFieldAllowed[field] {
			violations = append(violations, ConfigError{
				Field:   fmt.Sprintf("generation.logging.correlation_fields[%d]", index),
				Message: fmt.Sprintf("unsupported correlation field %q; allowed: request_id, trace_id, span_id", field),
			})
		}
	}

	for name := range BuiltinCredentialDenylist {
		if !slices.Contains(target.redactFields, name) {
			violations = append(violations, ConfigError{
				Field:   "generation.logging.redact_fields",
				Message: fmt.Sprintf("cannot disable built-in redaction entry %q", name),
			})
		}
	}

	return violations
}

func validateNamespace(namespace string) []ConfigError {
	if namespace == "" {
		return nil
	}
	if len(namespace) > MaxNamespaceLength {
		return []ConfigError{{
			Field:   "generation.metrics.namespace",
			Message: fmt.Sprintf("namespace must be at most %d characters", MaxNamespaceLength),
		}}
	}
	if !namespacePattern.MatchString(namespace) {
		return []ConfigError{{
			Field:   "generation.metrics.namespace",
			Message: "namespace must match [a-zA-Z_:][a-zA-Z0-9_:]*",
		}}
	}
	return nil
}

func validateBuckets(buckets []float64) []ConfigError {
	if len(buckets) < 1 {
		return []ConfigError{{
			Field:   "generation.metrics.histogram_buckets_seconds",
			Message: "at least one bucket boundary is required",
		}}
	}
	if len(buckets) > MaxHistogramBuckets {
		return []ConfigError{{
			Field:   "generation.metrics.histogram_buckets_seconds",
			Message: fmt.Sprintf("at most %d bucket boundaries are allowed", MaxHistogramBuckets),
		}}
	}
	for index, bucket := range buckets {
		field := fmt.Sprintf("generation.metrics.histogram_buckets_seconds[%d]", index)
		if math.IsNaN(bucket) || math.IsInf(bucket, 0) {
			return []ConfigError{{Field: field, Message: "bucket boundaries must be finite"}}
		}
		if bucket <= 0 {
			return []ConfigError{{Field: field, Message: "bucket boundaries must be positive"}}
		}
		if index > 0 && bucket <= buckets[index-1] {
			return []ConfigError{{Field: field, Message: "bucket boundaries must be strictly increasing"}}
		}
	}
	return nil
}

func validateQuantiles(quantiles []float64) []ConfigError {
	if len(quantiles) < 1 {
		return []ConfigError{{
			Field:   "generation.metrics.summaries.quantiles",
			Message: "at least one quantile is required",
		}}
	}
	if len(quantiles) > MaxQuantiles {
		return []ConfigError{{
			Field:   "generation.metrics.summaries.quantiles",
			Message: fmt.Sprintf("at most %d quantiles are allowed", MaxQuantiles),
		}}
	}
	for index, quantile := range quantiles {
		field := fmt.Sprintf("generation.metrics.summaries.quantiles[%d]", index)
		if math.IsNaN(quantile) || math.IsInf(quantile, 0) {
			return []ConfigError{{Field: field, Message: "quantiles must be finite"}}
		}
		if quantile < 0 || quantile > 1 {
			return []ConfigError{{Field: field, Message: "quantiles must be within [0, 1]"}}
		}
		if index > 0 && quantile <= quantiles[index-1] {
			return []ConfigError{{Field: field, Message: "quantiles must be strictly increasing"}}
		}
	}
	return nil
}

func validateLimits(path string, value, hardCap int64) []ConfigError {
	if value < 1 {
		return []ConfigError{{Field: path, Message: "must be a positive integer"}}
	}
	if value > hardCap {
		return []ConfigError{{
			Field:   path,
			Message: fmt.Sprintf("must not exceed the hard safety ceiling of %d", hardCap),
		}}
	}
	return nil
}

// normalize canonicalizes signals, correlation fields and redaction
// fields. Signal and correlation lists keep their input order so error
// paths stay stable; redaction names are sorted. Unknown signals are
// preserved here so validation can reject them with an exact index.
func normalize(target *effective) {
	target.signals = normalizeSignals(target.signals)
	target.correlationFields = normalizeCorrelationFields(target.correlationFields)
	target.redactFields = normalizeRedactFields(target.redactFields)
}

func normalizeSignals(signals []string) []string {
	if len(signals) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(signals))
	result := make([]string, 0, len(signals))
	for _, signal := range signals {
		signal = strings.ToLower(strings.TrimSpace(signal))
		if signal == "" || seen[signal] {
			continue
		}
		seen[signal] = true
		result = append(result, signal)
	}
	return result
}

func normalizeCorrelationFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(fields))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	return result
}

func normalizeRedactFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(fields))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	sort.Strings(result)
	return result
}

// orderSignals reorders a validated signal list into the fixed contract
// order. Only called after validation, so every entry is known.
func orderSignals(signals []string) []string {
	seen := make(map[string]bool, len(signals))
	for _, signal := range signals {
		seen[signal] = true
	}
	result := make([]string, 0, len(signals))
	for _, name := range signalOrder {
		if seen[name] {
			result = append(result, name)
		}
	}
	return result
}

func (target *effective) build() *Policy {
	return &Policy{
		OutputDir: target.outputDir,
		Signals:   append([]string(nil), orderSignals(target.signals)...),
		Strict:    target.strict,
		Metrics: MetricsPolicy{
			Namespace:               target.namespace,
			HistogramBucketsSeconds: append([]float64(nil), target.buckets...),
			IncludeInFlightGauges:   target.includeInFlightGauges,
			MaxInstruments:          target.maxInstruments,
			MaxEstimatedSeries:      target.maxEstimatedSeries,
			Summaries: SummariesPolicy{
				Enabled:   target.summariesEnabled,
				Quantiles: append([]float64(nil), target.quantiles...),
			},
		},
		Tracing: TracingPolicy{
			IncludeInternalCalls:     target.includeInternalCalls,
			RecordExceptionEvents:    target.recordExceptionEvents,
			SemanticConventionsVersion: BuiltinSemanticConventionsVersion,
		},
		Logging: LoggingPolicy{
			EmitStartEvents:      target.emitStartEvents,
			EmitCompletionEvents: target.emitCompletionEvents,
			EmitDependencyErrors: target.emitDependencyErrors,
			CorrelationFields:    append([]string(nil), target.correlationFields...),
			RedactFields:         append([]string(nil), target.redactFields...),
		},
	}
}

package policy

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// TestResolveZeroConfigAppliesDocumentedDefaults is AC1: an absent
// generation node resolves to the documented defaults.
func TestResolveZeroConfigAppliesDocumentedDefaults(t *testing.T) {
	policy, err := Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve(nil, nil) failed: %v", err)
	}
	if policy == nil {
		t.Fatal("Resolve(nil, nil) returned nil policy")
	}
	if policy.OutputDir != "generate" {
		t.Errorf("OutputDir = %q, want generate", policy.OutputDir)
	}
	if want := []string{"metrics", "tracing", "logging"}; !reflect.DeepEqual(policy.Signals, want) {
		t.Errorf("Signals = %v, want %v", policy.Signals, want)
	}
	if policy.Strict {
		t.Error("Strict = true, want false")
	}
	if policy.Metrics.Namespace != "" {
		t.Errorf("Namespace = %q, want empty", policy.Metrics.Namespace)
	}
	if !reflect.DeepEqual(policy.Metrics.HistogramBucketsSeconds, DefaultHistogramBucketsSeconds) {
		t.Errorf("HistogramBucketsSeconds = %v, want defaults", policy.Metrics.HistogramBucketsSeconds)
	}
	if !policy.Metrics.IncludeInFlightGauges {
		t.Error("IncludeInFlightGauges = false, want true")
	}
	if policy.Metrics.MaxInstruments != DefaultMaxInstruments {
		t.Errorf("MaxInstruments = %d, want %d", policy.Metrics.MaxInstruments, DefaultMaxInstruments)
	}
	if policy.Metrics.MaxEstimatedSeries != DefaultMaxEstimatedSeries {
		t.Errorf("MaxEstimatedSeries = %d, want %d", policy.Metrics.MaxEstimatedSeries, DefaultMaxEstimatedSeries)
	}
	if policy.Metrics.Summaries.Enabled {
		t.Error("Summaries.Enabled = true, want false")
	}
	if !reflect.DeepEqual(policy.Metrics.Summaries.Quantiles, DefaultQuantiles) {
		t.Errorf("Summaries.Quantiles = %v, want defaults", policy.Metrics.Summaries.Quantiles)
	}
	if policy.Tracing.IncludeInternalCalls {
		t.Error("IncludeInternalCalls = true, want false")
	}
	if !policy.Tracing.RecordExceptionEvents {
		t.Error("RecordExceptionEvents = false, want true")
	}
	if policy.Tracing.SemanticConventionsVersion != "1.37.0" {
		t.Errorf("SemanticConventionsVersion = %q, want 1.37.0", policy.Tracing.SemanticConventionsVersion)
	}
	if policy.Logging.EmitStartEvents {
		t.Error("EmitStartEvents = true, want false")
	}
	if !policy.Logging.EmitCompletionEvents {
		t.Error("EmitCompletionEvents = false, want true")
	}
	if !policy.Logging.EmitDependencyErrors {
		t.Error("EmitDependencyErrors = false, want true")
	}
	if !reflect.DeepEqual(policy.Logging.CorrelationFields, DefaultCorrelationFields) {
		t.Errorf("CorrelationFields = %v, want defaults", policy.Logging.CorrelationFields)
	}
	if !reflect.DeepEqual(policy.Logging.RedactFields, DefaultRedactFields) {
		t.Errorf("RedactFields = %v, want defaults", policy.Logging.RedactFields)
	}
}

// TestResolveExplicitFalseSurvivesDefaults is AC3: an explicit false in
// YAML is never overwritten by a default true.
func TestResolveExplicitFalseSurvivesDefaults(t *testing.T) {
	config := &GenerationConfig{
		Metrics: &MetricsConfig{IncludeInFlightGauges: new(false)},
		Tracing: &TracingConfig{RecordExceptionEvents: new(false)},
		Logging: &LoggingConfig{EmitCompletionEvents: new(false)},
	}
	resolved, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.Metrics.IncludeInFlightGauges {
		t.Error("IncludeInFlightGauges = true, want explicit false")
	}
	if resolved.Tracing.RecordExceptionEvents {
		t.Error("RecordExceptionEvents = true, want explicit false")
	}
	if resolved.Logging.EmitCompletionEvents {
		t.Error("EmitCompletionEvents = true, want explicit false")
	}
}

// TestResolveSummaryQuantilesReadOnlyWhenEnabled: an invalid quantile list
// is ignored when summaries are disabled and rejected when enabled.
func TestResolveSummaryQuantilesReadOnlyWhenEnabled(t *testing.T) {
	invalidQuantiles := []float64{0.9, 0.5}
	config := &GenerationConfig{
		Metrics: &MetricsConfig{
			Summaries: &SummariesConfig{Enabled: new(false), Quantiles: invalidQuantiles},
		},
	}
	resolved, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve with summaries disabled should ignore quantiles: %v", err)
	}
	if !reflect.DeepEqual(resolved.Metrics.Summaries.Quantiles, DefaultQuantiles) {
		t.Errorf("disabled summaries must keep default quantiles, got %v", resolved.Metrics.Summaries.Quantiles)
	}

	enabled := &GenerationConfig{
		Metrics: &MetricsConfig{
			Summaries: &SummariesConfig{Enabled: new(true), Quantiles: invalidQuantiles},
		},
	}
	if _, err := Resolve(enabled, nil); err == nil {
		t.Fatal("enabled summaries with descending quantiles must fail")
	}
}

// TestResolveRedactFieldsNormalized: entries are lowercased, trimmed,
// deduplicated and sorted; built-in denylist entries are always present.
func TestResolveRedactFieldsNormalized(t *testing.T) {
	config := &GenerationConfig{
		Logging: &LoggingConfig{
			RedactFields: []string{"API_KEY", " Password ", "password", "email", "authorization", "cookie", "secret", "token"},
		},
	}
	resolved, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	want := []string{"api_key", "authorization", "cookie", "email", "password", "secret", "token"}
	if !reflect.DeepEqual(resolved.Logging.RedactFields, want) {
		t.Errorf("RedactFields = %v, want %v", resolved.Logging.RedactFields, want)
	}
}

// TestResolveSignalsNormalized: duplicates collapse into the fixed order.
func TestResolveSignalsNormalized(t *testing.T) {
	config := &GenerationConfig{Signals: []string{"logging", "metrics", "metrics", "TRACING"}}
	resolved, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	want := []string{"metrics", "tracing", "logging"}
	if !reflect.DeepEqual(resolved.Signals, want) {
		t.Errorf("Signals = %v, want %v", resolved.Signals, want)
	}
}

// TestResolveRejectsInvalidValues is AC4: every invalid value fails with
// an exact field path and the GEN_INVALID_CONFIG code.
func TestResolveRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		config *GenerationConfig
		field  string
	}{
		{"descending buckets", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{0.1, 0.05}}}, "generation.metrics.histogram_buckets_seconds[1]"},
		{"equal buckets", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{0.1, 0.1}}}, "generation.metrics.histogram_buckets_seconds[1]"},
		{"zero bucket", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{0}}}, "generation.metrics.histogram_buckets_seconds[0]"},
		{"negative bucket", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{-1}}}, "generation.metrics.histogram_buckets_seconds[0]"},
		{"too many buckets", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: ascending(51)}}, "generation.metrics.histogram_buckets_seconds"},
		{"empty buckets", &GenerationConfig{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{}}}, "generation.metrics.histogram_buckets_seconds"},
		{"duplicate quantile", &GenerationConfig{Metrics: &MetricsConfig{Summaries: &SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5, 0.5}}}}, "generation.metrics.summaries.quantiles[1]"},
		{"quantile out of range", &GenerationConfig{Metrics: &MetricsConfig{Summaries: &SummariesConfig{Enabled: new(true), Quantiles: []float64{1.5}}}}, "generation.metrics.summaries.quantiles[0]"},
		{"too many quantiles", &GenerationConfig{Metrics: &MetricsConfig{Summaries: &SummariesConfig{Enabled: new(true), Quantiles: ascendingFloat(11)}}}, "generation.metrics.summaries.quantiles"},
		{"unknown signal", &GenerationConfig{Signals: []string{"metrics", "traces"}}, "generation.signals[1]"},
		{"empty signals", &GenerationConfig{Signals: []string{}}, "generation.signals"},
		{"empty output dir", &GenerationConfig{OutputDir: new("")}, "generation.output_dir"},
		{"whitespace output dir", &GenerationConfig{OutputDir: new("  ")}, "generation.output_dir"},
		{"output dir with NUL", &GenerationConfig{OutputDir: new("gen\x00erate")}, "generation.output_dir"},
		{"bad namespace", &GenerationConfig{Metrics: &MetricsConfig{Namespace: "my-namespace"}}, "generation.metrics.namespace"},
		{"too long namespace", &GenerationConfig{Metrics: &MetricsConfig{Namespace: string(longNamespace())}}, "generation.metrics.namespace"},
		{"zero instruments", &GenerationConfig{Metrics: &MetricsConfig{MaxInstruments: new(int64(0))}}, "generation.metrics.max_instruments"},
		{"negative series", &GenerationConfig{Metrics: &MetricsConfig{MaxEstimatedSeries: new(int64(-5))}}, "generation.metrics.max_estimated_series"},
		{"over hard cap instruments", &GenerationConfig{Metrics: &MetricsConfig{MaxInstruments: new(int64(HardMaxInstruments + 1))}}, "generation.metrics.max_instruments"},
		{"over hard cap series", &GenerationConfig{Metrics: &MetricsConfig{MaxEstimatedSeries: new(int64(HardMaxEstimatedSeries + 1))}}, "generation.metrics.max_estimated_series"},
		{"unknown correlation field", &GenerationConfig{Logging: &LoggingConfig{CorrelationFields: []string{"request_id", "user_id"}}}, "generation.logging.correlation_fields[1]"},
		{"redact removes password", &GenerationConfig{Logging: &LoggingConfig{RedactFields: []string{"authorization", "cookie", "secret", "token"}}}, "generation.logging.redact_fields"},
		{"redact removes denylist entry", &GenerationConfig{Logging: &LoggingConfig{RedactFields: []string{}}}, "generation.logging.redact_fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(test.config, nil)
			if err == nil {
				t.Fatalf("Resolve accepted invalid config %v", test.config)
			}
			var failures *ConfigErrors
			if !errors.As(err, &failures) {
				t.Fatalf("error type = %T, want *ConfigErrors", err)
			}
			found := false
			for _, violation := range failures.Violations() {
				if violation.Field == test.field {
					found = true
				}
			}
			if !found {
				t.Errorf("no violation at field %q; violations: %v", test.field, failures.Violations())
			}
			if !containsCode(err.Error(), CodeInvalidConfig) {
				t.Errorf("error %q missing code %s", err.Error(), CodeInvalidConfig)
			}
		})
	}
}

// TestConfigErrorsNeverLeakConfigValues: error output contains field paths
// and rule messages only, never configuration values.
func TestConfigErrorsNeverLeakConfigValues(t *testing.T) {
	secret := "hunter2-super-secret-value"
	config := &GenerationConfig{
		Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{0.5, -1}},
		Logging: &LoggingConfig{RedactFields: []string{secret}},
	}
	_, err := Resolve(config, nil)
	if err == nil {
		t.Fatal("expected a config error")
	}
	if contains(err.Error(), secret) {
		t.Fatalf("error leaks configuration value: %s", err.Error())
	}
}

// TestResolveAggregatesEveryViolation: multiple invalid fields are all
// reported in one error.
func TestResolveAggregatesEveryViolation(t *testing.T) {
	config := &GenerationConfig{
		Signals: []string{},
		Metrics: &MetricsConfig{
			HistogramBucketsSeconds: []float64{0.1, 0.05},
			MaxInstruments:          new(int64(0)),
		},
	}
	_, err := Resolve(config, nil)
	var failures *ConfigErrors
	if !errors.As(err, &failures) {
		t.Fatalf("error type = %T, want *ConfigErrors", err)
	}
	if len(failures.Violations()) < 3 {
		t.Errorf("expected at least 3 violations, got %d: %v", len(failures.Violations()), failures)
	}
}

// TestResolveDoesNotMutateInput: the input configuration stays untouched.
func TestResolveDoesNotMutateInput(t *testing.T) {
	config := &GenerationConfig{
		Signals: []string{"logging", "metrics"},
		Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{0.1, 0.2}},
		Logging: &LoggingConfig{RedactFields: []string{"Authorization", "authorization", "cookie", "password", "secret", "token"}},
	}
	before := fmt.Sprintf("%+v", config)
	if _, err := Resolve(config, nil); err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if after := fmt.Sprintf("%+v", config); after != before {
		t.Fatalf("Resolve mutated its input: before %s, after %s", before, after)
	}
}

// TestPolicySlicesAreCopies: mutating a resolved policy slice never
// affects a second policy built from the same config.
func TestPolicySlicesAreCopies(t *testing.T) {
	config := &GenerationConfig{
		Signals:               []string{"metrics"},
		Metrics:               &MetricsConfig{HistogramBucketsSeconds: []float64{0.1}},
		Logging:               &LoggingConfig{RedactFields: []string{"authorization", "cookie", "password", "secret", "token", "extra"}},
	}
	first, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	second, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	first.Signals[0] = "tracing"
	first.Metrics.HistogramBucketsSeconds[0] = 99
	first.Logging.RedactFields[0] = "mutated"
	if second.Signals[0] != "metrics" {
		t.Error("policy slices are shared between resolutions")
	}
	if second.Metrics.HistogramBucketsSeconds[0] != 0.1 {
		t.Error("bucket slice is shared between resolutions")
	}
	if second.Logging.RedactFields[0] != "authorization" {
		t.Error("redact slice is shared between resolutions")
	}
}

// TestResolveEnvironmentIndependence is AC5: identical configuration
// resolves identically regardless of environment variables, working
// directory or user name.
func TestResolveEnvironmentIndependence(t *testing.T) {
	config := &GenerationConfig{
		OutputDir: new("out"),
		Metrics:   &MetricsConfig{HistogramBucketsSeconds: []float64{0.1, 0.5}},
		Logging:   &LoggingConfig{RedactFields: []string{"extra", "authorization", "cookie", "password", "secret", "token"}},
	}
	first, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Same configuration under a different working directory, user and
	// environment variables must yield the identical policy and digest.
	t.Setenv("USER", "someone-else")
	t.Setenv("HOME", "/home/someone-else")
	t.Setenv("SI_SEMANTIC_CONVENTIONS_VERSION", "9.9.9")
	t.Chdir(t.TempDir())
	second, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("digest differs across identical resolutions: %s vs %s", first.Digest(), second.Digest())
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("policies differ across identical resolutions:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func ascending(count int) []float64 {
	values := make([]float64, count)
	for index := range values {
		values[index] = float64(index + 1)
	}
	return values
}

func ascendingFloat(count int) []float64 {
	values := make([]float64, count)
	for index := range values {
		values[index] = float64(index+1) / float64(count+1)
	}
	return values
}

func longNamespace() string {
	value := make([]byte, MaxNamespaceLength+1)
	for index := range value {
		value[index] = 'a'
	}
	return string(value)
}

func containsCode(message, code string) bool {
	return len(message) >= len(code) && message[:len(code)] == code
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

package policy

import "testing"

// TestDigestStableAcrossResolutions: the same configuration always yields
// the same digest, independent of resolution order or working directory.
func TestDigestStableAcrossResolutions(t *testing.T) {
	config := &GenerationConfig{
		Signals: []string{"logging", "metrics"},
		Metrics: &MetricsConfig{
			Namespace:               "api",
			HistogramBucketsSeconds: []float64{0.1, 0.5},
		},
		Logging: &LoggingConfig{
			RedactFields: []string{"authorization", "cookie", "password", "secret", "token", "extra"},
		},
	}
	var previous string
	for iteration := 0; iteration < 10; iteration++ {
		resolved, err := Resolve(config, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		digest := resolved.Digest()
		if previous != "" && digest != previous {
			t.Fatalf("digest changed between iterations: %s vs %s", previous, digest)
		}
		previous = digest
	}
}

// TestDigestIgnoresOutputDir: two policies differing only in output
// directory have identical digests.
func TestDigestIgnoresOutputDir(t *testing.T) {
	first, err := Resolve(&GenerationConfig{OutputDir: new("a")}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	second, err := Resolve(&GenerationConfig{OutputDir: new("/tmp/b")}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if first.Digest() != second.Digest() {
		t.Fatalf("digest must ignore output_dir: %s vs %s", first.Digest(), second.Digest())
	}
}

// TestDigestChangesWithPolicy: every semantic field feeds the digest.
func TestDigestChangesWithPolicy(t *testing.T) {
	baseline, err := Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	variants := []*GenerationConfig{
		{Signals: []string{"metrics"}},
		{Strict: new(true)},
		{Metrics: &MetricsConfig{Namespace: "api"}},
		{Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{1}}},
		{Metrics: &MetricsConfig{IncludeInFlightGauges: new(false)}},
		{Metrics: &MetricsConfig{MaxInstruments: new(int64(1))}},
		{Metrics: &MetricsConfig{MaxEstimatedSeries: new(int64(1))}},
		{Metrics: &MetricsConfig{Summaries: &SummariesConfig{Enabled: new(true)}}},
		{Tracing: &TracingConfig{IncludeInternalCalls: new(true)}},
		{Tracing: &TracingConfig{RecordExceptionEvents: new(false)}},
		{Logging: &LoggingConfig{EmitStartEvents: new(true)}},
		{Logging: &LoggingConfig{EmitCompletionEvents: new(false)}},
		{Logging: &LoggingConfig{EmitDependencyErrors: new(false)}},
		{Logging: &LoggingConfig{CorrelationFields: []string{"request_id"}}},
		{Logging: &LoggingConfig{RedactFields: []string{"authorization", "cookie", "password", "secret", "token", "extra"}}},
	}
	for index, variant := range variants {
		resolved, err := Resolve(variant, nil)
		if err != nil {
			t.Fatalf("variant %d failed: %v", index, err)
		}
		if resolved.Digest() == baseline.Digest() {
			t.Errorf("variant %d (%+v) produced the baseline digest", index, variant)
		}
	}
}

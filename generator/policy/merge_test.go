package policy

import (
	"reflect"
	"testing"
)

// TestMergePriorityPerField is AC2: CLI flags override si.yaml, si.yaml
// overrides defaults, and unset CLI fields keep the YAML value.
func TestMergePriorityPerField(t *testing.T) {
	yamlSignals := []string{"logging"}
	yamlBuckets := []float64{0.1, 0.5}
	cliBuckets := []float64{0.01, 0.02}
	config := &GenerationConfig{
		OutputDir: new("yaml-out"),
		Signals:   yamlSignals,
		Strict:    new(true),
		Metrics: &MetricsConfig{
			Namespace:               "api",
			HistogramBucketsSeconds: yamlBuckets,
			IncludeInFlightGauges:   new(false),
			MaxInstruments:          new(int64(500)),
		},
		Logging: &LoggingConfig{EmitStartEvents: new(true)},
	}
	overrides := &Overrides{
		OutputDir: new("cli-out"),
		Strict:    new(false),
		Metrics: MetricsOverrides{
			HistogramBucketsSeconds: cliBuckets,
			MaxInstruments:          new(int64(999)),
		},
	}
	resolved, err := Resolve(config, overrides)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// CLI values win.
	if resolved.OutputDir != "cli-out" {
		t.Errorf("OutputDir = %q, want CLI value cli-out", resolved.OutputDir)
	}
	if resolved.Strict {
		t.Error("Strict = true, want CLI value false")
	}
	if !reflect.DeepEqual(resolved.Metrics.HistogramBucketsSeconds, cliBuckets) {
		t.Errorf("HistogramBucketsSeconds = %v, want CLI buckets", resolved.Metrics.HistogramBucketsSeconds)
	}
	if resolved.Metrics.MaxInstruments != 999 {
		t.Errorf("MaxInstruments = %d, want CLI value 999", resolved.Metrics.MaxInstruments)
	}
	// Unset CLI fields keep the YAML value.
	if resolved.Metrics.Namespace != "api" {
		t.Errorf("Namespace = %q, want YAML value api", resolved.Metrics.Namespace)
	}
	if resolved.Metrics.IncludeInFlightGauges {
		t.Error("IncludeInFlightGauges = true, want YAML value false")
	}
	if !reflect.DeepEqual(resolved.Signals, []string{"logging"}) {
		t.Errorf("Signals = %v, want YAML signals", resolved.Signals)
	}
	if !resolved.Logging.EmitStartEvents {
		t.Error("EmitStartEvents = false, want YAML value true")
	}
}

// TestMergeDefaultsYAMLCLIMatrix covers the defaults/YAML/CLI three-layer
// combination for every mergeable field.
func TestMergeDefaultsYAMLCLIMatrix(t *testing.T) {
	tests := []struct {
		name      string
		config    *GenerationConfig
		overrides *Overrides
		check     func(*testing.T, *Policy)
	}{
		{
			name: "output_dir defaults to generate",
			check: func(t *testing.T, resolved *Policy) {
				if resolved.OutputDir != "generate" {
					t.Errorf("OutputDir = %q, want generate", resolved.OutputDir)
				}
			},
		},
		{
			name:   "output_dir YAML wins over default",
			config: &GenerationConfig{OutputDir: new("artifacts")},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.OutputDir != "artifacts" {
					t.Errorf("OutputDir = %q, want artifacts", resolved.OutputDir)
				}
			},
		},
		{
			name:      "output_dir CLI wins over YAML",
			config:    &GenerationConfig{OutputDir: new("artifacts")},
			overrides: &Overrides{OutputDir: new("cli")},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.OutputDir != "cli" {
					t.Errorf("OutputDir = %q, want cli", resolved.OutputDir)
				}
			},
		},
		{
			name:   "signals YAML wins over default",
			config: &GenerationConfig{Signals: []string{"metrics"}},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Signals, []string{"metrics"}) {
					t.Errorf("Signals = %v, want [metrics]", resolved.Signals)
				}
			},
		},
		{
			name:      "signals CLI wins over YAML",
			config:    &GenerationConfig{Signals: []string{"metrics"}},
			overrides: &Overrides{Signals: []string{"tracing"}},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Signals, []string{"tracing"}) {
					t.Errorf("Signals = %v, want [tracing]", resolved.Signals)
				}
			},
		},
		{
			name:   "strict YAML true",
			config: &GenerationConfig{Strict: new(true)},
			check: func(t *testing.T, resolved *Policy) {
				if !resolved.Strict {
					t.Error("Strict = false, want true")
				}
			},
		},
		{
			name:      "strict CLI false wins over YAML true",
			config:    &GenerationConfig{Strict: new(true)},
			overrides: &Overrides{Strict: new(false)},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.Strict {
					t.Error("Strict = true, want CLI false")
				}
			},
		},
		{
			name: "namespace YAML wins over empty default",
			config: &GenerationConfig{
				Metrics: &MetricsConfig{Namespace: "service"},
			},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.Metrics.Namespace != "service" {
					t.Errorf("Namespace = %q, want service", resolved.Metrics.Namespace)
				}
			},
		},
		{
			name: "buckets YAML wins over default",
			config: &GenerationConfig{
				Metrics: &MetricsConfig{HistogramBucketsSeconds: []float64{1, 2}},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Metrics.HistogramBucketsSeconds, []float64{1, 2}) {
					t.Errorf("buckets = %v, want [1 2]", resolved.Metrics.HistogramBucketsSeconds)
				}
			},
		},
		{
			name: "gauges YAML false wins over default true",
			config: &GenerationConfig{
				Metrics: &MetricsConfig{IncludeInFlightGauges: new(false)},
			},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.Metrics.IncludeInFlightGauges {
					t.Error("IncludeInFlightGauges = true, want false")
				}
			},
		},
		{
			name: "summaries enabled YAML true with default quantiles",
			config: &GenerationConfig{
				Metrics: &MetricsConfig{Summaries: &SummariesConfig{Enabled: new(true)}},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !resolved.Metrics.Summaries.Enabled {
					t.Error("Summaries.Enabled = false, want true")
				}
				if !reflect.DeepEqual(resolved.Metrics.Summaries.Quantiles, DefaultQuantiles) {
					t.Errorf("Quantiles = %v, want defaults", resolved.Metrics.Summaries.Quantiles)
				}
			},
		},
		{
			name: "quantiles CLI wins over YAML",
			config: &GenerationConfig{
				Metrics: &MetricsConfig{
					Summaries: &SummariesConfig{Enabled: new(true), Quantiles: []float64{0.5}},
				},
			},
			overrides: &Overrides{
				Metrics: MetricsOverrides{Summaries: SummariesOverrides{Quantiles: []float64{0.9}}},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Metrics.Summaries.Quantiles, []float64{0.9}) {
					t.Errorf("Quantiles = %v, want CLI [0.9]", resolved.Metrics.Summaries.Quantiles)
				}
			},
		},
		{
			name: "internal calls YAML true wins over default false",
			config: &GenerationConfig{
				Tracing: &TracingConfig{IncludeInternalCalls: new(true)},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !resolved.Tracing.IncludeInternalCalls {
					t.Error("IncludeInternalCalls = false, want true")
				}
			},
		},
		{
			name: "exception events CLI false wins over YAML true",
			config: &GenerationConfig{
				Tracing: &TracingConfig{RecordExceptionEvents: new(true)},
			},
			overrides: &Overrides{
				Tracing: TracingOverrides{RecordExceptionEvents: new(false)},
			},
			check: func(t *testing.T, resolved *Policy) {
				if resolved.Tracing.RecordExceptionEvents {
					t.Error("RecordExceptionEvents = true, want CLI false")
				}
			},
		},
		{
			name: "start events YAML true wins over default false",
			config: &GenerationConfig{
				Logging: &LoggingConfig{EmitStartEvents: new(true)},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !resolved.Logging.EmitStartEvents {
					t.Error("EmitStartEvents = false, want true")
				}
			},
		},
		{
			name: "correlation fields YAML wins over default",
			config: &GenerationConfig{
				Logging: &LoggingConfig{CorrelationFields: []string{"trace_id"}},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Logging.CorrelationFields, []string{"trace_id"}) {
					t.Errorf("CorrelationFields = %v, want [trace_id]", resolved.Logging.CorrelationFields)
				}
			},
		},
		{
			name: "redact CLI wins over YAML",
			config: &GenerationConfig{
				Logging: &LoggingConfig{RedactFields: []string{"email", "authorization", "cookie", "password", "secret", "token"}},
			},
			overrides: &Overrides{
				Logging: LoggingOverrides{RedactFields: []string{"phone", "authorization", "cookie", "password", "secret", "token"}},
			},
			check: func(t *testing.T, resolved *Policy) {
				if !reflect.DeepEqual(resolved.Logging.RedactFields, []string{"authorization", "cookie", "password", "phone", "secret", "token"}) {
					t.Errorf("RedactFields = %v, want CLI set", resolved.Logging.RedactFields)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := Resolve(test.config, test.overrides)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			test.check(t, resolved)
		})
	}
}

// TestMergeOverridesRespectValidation: CLI-provided values are validated
// with the same rules as YAML values.
func TestMergeOverridesRespectValidation(t *testing.T) {
	overrides := &Overrides{Metrics: MetricsOverrides{HistogramBucketsSeconds: []float64{5, 1}}}
	if _, err := Resolve(nil, overrides); err == nil {
		t.Fatal("invalid CLI buckets must fail resolution")
	}
}

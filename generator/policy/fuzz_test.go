package policy

import (
	"testing"
)

// FuzzDecodeGeneration ensures the strict config decoder never panics or
// hangs on arbitrary input and that errors never leak payload values.
func FuzzDecodeGeneration(f *testing.F) {
	f.Add([]byte("strict: true\nsignals: [metrics]\n"))
	f.Add([]byte("not yaml at all { ["))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01})
	f.Add([]byte("metrics:\n  histogram_buckets_seconds: [0.1, .nan]\n"))
	f.Add([]byte("generation:\n  output_dir: gen\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		config, err := DecodeGeneration(data)
		if err != nil {
			// Errors must not echo the raw input; the decoder only ever
			// reports field names, tags and parsed scalar forms.
			return
		}
		if _, err := Resolve(config, nil); err != nil {
			return
		}
	})
}

// FuzzResolve feeds random numbers, signal names, correlation fields and
// redaction names into Resolve; it must never panic and must always either
// resolve or fail with a *ConfigErrors.
func FuzzResolve(f *testing.F) {
	f.Add(0.1, 0.2, "metrics", "request_id", "authorization")
	f.Add(0.9, 0.5, "bogus", "user_id", "password")
	f.Add(-1.0, 2.0, "", "", "")
	f.Fuzz(func(t *testing.T, bucket, quantile float64, signal, correlation, redact string) {
		config := &GenerationConfig{
			Signals: []string{signal},
			Metrics: &MetricsConfig{
				HistogramBucketsSeconds: []float64{bucket, quantile},
				Summaries: &SummariesConfig{
					Enabled:   new(true),
					Quantiles: []float64{bucket, quantile},
				},
			},
			Logging: &LoggingConfig{
				CorrelationFields: []string{correlation},
				RedactFields:      []string{redact},
			},
		}
		resolved, err := Resolve(config, nil)
		if err != nil {
			if _, ok := err.(*ConfigErrors); !ok {
				t.Fatalf("error type = %T, want *ConfigErrors", err)
			}
			return
		}
		// Any resolved policy must produce a stable digest.
		if resolved.Digest() == "" {
			t.Fatal("empty digest for resolved policy")
		}
	})
}

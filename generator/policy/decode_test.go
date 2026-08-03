package policy

import (
	"strings"
	"testing"
)

func TestDecodeConfigFileWithoutGenerationNode(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"empty file", ""},
		{"blank file", "  \n"},
		{"scan config only", "service:\n  name: demo\n"},
		{"unrelated node", "include_tests: true\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := DecodeConfigFile([]byte(test.data))
			if err != nil {
				t.Fatalf("DecodeConfigFile failed: %v", err)
			}
			if config != nil {
				t.Fatalf("expected nil generation config, got %+v", config)
			}
		})
	}
}

func TestDecodeConfigFileExtractsGenerationNode(t *testing.T) {
	data := "service:\n  name: demo\ninclude_tests: true\ngeneration:\n  strict: true\n  signals: [metrics]\n"
	config, err := DecodeConfigFile([]byte(data))
	if err != nil {
		t.Fatalf("DecodeConfigFile failed: %v", err)
	}
	if config == nil {
		t.Fatal("expected a generation config")
	}
	if config.Strict == nil || !*config.Strict {
		t.Error("Strict not decoded from generation node")
	}
	if len(config.Signals) != 1 || config.Signals[0] != "metrics" {
		t.Errorf("Signals = %v, want [metrics]", config.Signals)
	}
}

func TestDecodeConfigFileRejectsUnknownGenerationField(t *testing.T) {
	data := "generation:\n  output_dir: gen\n  semantic_conventions_version: 1.38.0\n"
	_, err := DecodeConfigFile([]byte(data))
	if err == nil {
		t.Fatal("unknown generation field must be rejected")
	}
	if !strings.Contains(err.Error(), "semantic_conventions_version") {
		t.Errorf("error %q does not name the unknown field", err.Error())
	}
	if !strings.Contains(err.Error(), "generation") {
		t.Errorf("error %q does not carry the generation document prefix", err.Error())
	}
}

func TestDecodeConfigFileRejectsDuplicateGenerationKey(t *testing.T) {
	data := "generation:\n  strict: true\ngeneration:\n  strict: false\n"
	_, err := DecodeConfigFile([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("duplicate generation key must fail, got %v", err)
	}
}

func TestDecodeGenerationRejectsStrictViolations(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{"unknown field", "bogus: true\n", "bogus"},
		{"duplicate key", "strict: true\nstrict: false\n", "duplicate key"},
		{"wrong scalar type", "strict: maybe\n", "cannot unmarshal"},
		{"non-finite number", "metrics:\n  histogram_buckets_seconds: [0.1, .nan]\n", "non-finite number"},
		{"timestamp", "metrics:\n  histogram_buckets_seconds: [0.1]\n  updated: 2026-08-03T10:00:00Z\n", "timestamps are not allowed"},
		{"anchor", "metrics: &m\n  namespace: api\n", "anchor"},
		{"alias", "metrics:\n  <<: *m\n", "anchor"},
		{"multiple documents", "strict: true\n---\nstrict: false\n", "multiple YAML documents"},
		{"non-string mapping key", "1: two\n", "must be a string"},
		{"empty output dir type", "output_dir: 42\n", "cannot unmarshal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeGeneration([]byte(test.data))
			if err == nil {
				t.Fatalf("DecodeGeneration accepted %q", test.data)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), test.wantErr)
			}
			if !strings.Contains(err.Error(), "generation.") && !strings.Contains(err.Error(), "generation:") {
				t.Errorf("error %q lacks the generation path prefix", err.Error())
			}
		})
	}
}

func TestDecodeGenerationPathFormat(t *testing.T) {
	_, err := DecodeGeneration([]byte("metrics:\n  histogram_buckets_seconds: [0.1]\n  bogus: 1\n"))
	if err == nil {
		t.Fatal("unknown nested field must be rejected")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not name the unknown field", err.Error())
	}
	if !strings.Contains(err.Error(), "generation") {
		t.Errorf("error %q lacks the generation document prefix", err.Error())
	}
}

func TestDecodeGenerationFullConfigRoundTrip(t *testing.T) {
	data := `
output_dir: gen
signals: [metrics, tracing]
strict: false
metrics:
  namespace: api
  histogram_buckets_seconds: [0.01, 0.1, 1]
  include_in_flight_gauges: false
  max_instruments: 5000
  max_estimated_series: 50000
  summaries:
    enabled: true
    quantiles: [0.5, 0.9]
tracing:
  include_internal_calls: true
  record_exception_events: false
logging:
  emit_start_events: true
  emit_completion_events: false
  emit_dependency_errors: true
  correlation_fields: [request_id, trace_id]
  redact_fields: [authorization, cookie, password, secret, token, session_id]
`
	config, err := DecodeGeneration([]byte(data))
	if err != nil {
		t.Fatalf("DecodeGeneration failed: %v", err)
	}
	resolved, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved.OutputDir != "gen" {
		t.Errorf("OutputDir = %q, want gen", resolved.OutputDir)
	}
	if !resolved.Metrics.Summaries.Enabled {
		t.Error("Summaries.Enabled = false, want true")
	}
	if !resolved.Tracing.IncludeInternalCalls {
		t.Error("IncludeInternalCalls = false, want true")
	}
	if resolved.Logging.EmitCompletionEvents {
		t.Error("EmitCompletionEvents = true, want false")
	}
	if resolved.Metrics.MaxInstruments != 5000 {
		t.Errorf("MaxInstruments = %d, want 5000", resolved.Metrics.MaxInstruments)
	}
}

// TestDecodeGenerationBoolSemantics: explicit false in YAML decodes as a
// set pointer, never as "absent".
func TestDecodeGenerationBoolSemantics(t *testing.T) {
	config, err := DecodeGeneration([]byte("strict: false\n"))
	if err != nil {
		t.Fatalf("DecodeGeneration failed: %v", err)
	}
	if config.Strict == nil {
		t.Fatal("explicit false decoded as nil pointer")
	}
	if *config.Strict {
		t.Error("Strict = true, want false")
	}
}

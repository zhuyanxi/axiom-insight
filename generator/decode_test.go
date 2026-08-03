package generator

import (
	"strings"
	"testing"
)

func TestDecodeValidFixtures(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		decode  func([]byte) (interface{ Validate() []*ValidationError }, error)
	}{
		{
			name:    "metrics",
			fixture: "valid/metrics.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				document, err := DecodeMetrics(data)
				if err != nil {
					return nil, err
				}
				return document, nil
			},
		},
		{
			name:    "otel",
			fixture: "valid/otel.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				document, err := DecodeOTel(data)
				if err != nil {
					return nil, err
				}
				return document, nil
			},
		},
		{
			name:    "logging",
			fixture: "valid/logging.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				document, err := DecodeLogging(data)
				if err != nil {
					return nil, err
				}
				return document, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := test.decode(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("decode %s: %v", test.fixture, err)
			}
			if violations := document.Validate(); len(violations) > 0 {
				t.Fatalf("expected valid document, got violations: %v", violations)
			}
		})
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	// AC2: a misspelled field must not be ignored; the error must carry the
	// offending field name.
	_, err := DecodeMetrics(readFixture(t, "invalid/unknown-field.yaml"))
	if err == nil {
		t.Fatal("expected decode error for unknown field")
	}
	if !strings.Contains(err.Error(), "servce_name") {
		t.Fatalf("error must name the offending field, got: %v", err)
	}
}

func TestDecodeRejectsDuplicateKey(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/duplicate-key.yaml"))
	if err == nil {
		t.Fatal("expected decode error for duplicate key")
	}
	if !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsNonFinite(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/non-finite.yaml"))
	if err == nil {
		t.Fatal("expected decode error for non-finite number")
	}
	if !strings.Contains(err.Error(), "non-finite") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsAlias(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/alias.yaml"))
	if err == nil {
		t.Fatal("expected decode error for alias")
	}
	if !strings.Contains(err.Error(), "alias") && !strings.Contains(err.Error(), "anchor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsBadScalarType(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/bad-scalar-type.yaml"))
	if err == nil {
		t.Fatal("expected decode error for wrong scalar type")
	}
	if !strings.Contains(err.Error(), "unit") {
		t.Fatalf("error must point at the offending field, got: %v", err)
	}
}

func TestDecodeRejectsTimestamp(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/timestamp.yaml"))
	if err == nil {
		t.Fatal("expected decode error for timestamp scalar")
	}
	if !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsMultipleDocuments(t *testing.T) {
	_, err := DecodeMetrics(readFixture(t, "invalid/multiple-documents.yaml"))
	if err == nil {
		t.Fatal("expected decode error for multiple YAML documents")
	}
	if !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsAnchor(t *testing.T) {
	// Anchors are rejected even without an alias.
	_, err := DecodeMetrics([]byte(
		"schema_version: generator.metrics/v1\ndocument_type: instrumentation.metrics\n" +
			"source: &s\n  ir_schema_version: v1\n  service_name: orders\n" +
			"generated_by:\n  name: si\n  version: v0.2.0\nmetrics: []\n"))
	if err == nil {
		t.Fatal("expected decode error for anchor")
	}
	if !strings.Contains(err.Error(), "anchor") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsNonStringMappingKey(t *testing.T) {
	_, err := DecodeMetrics([]byte(
		"schema_version: generator.metrics/v1\ndocument_type: instrumentation.metrics\n" +
			"source:\n  ir_schema_version: v1\n  service_name: orders\n" +
			"generated_by:\n  name: si\n  version: v0.2.0\n" +
			"metrics:\n  1: not-a-valid-metric\n"))
	if err == nil {
		t.Fatal("expected decode error for non-string mapping key")
	}
}

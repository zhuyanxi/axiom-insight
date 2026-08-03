package generator

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderDeterministic(t *testing.T) {
	metrics := readFixture(t, "valid/metrics.yaml")
	otel := readFixture(t, "valid/otel.yaml")
	logging := readFixture(t, "valid/logging.yaml")

	renderers := []struct {
		name   string
		render func() ([]byte, error)
	}{
		{"metrics", func() ([]byte, error) {
			document, err := DecodeMetrics(metrics)
			if err != nil {
				return nil, err
			}
			return RenderMetrics(document)
		}},
		{"otel", func() ([]byte, error) {
			document, err := DecodeOTel(otel)
			if err != nil {
				return nil, err
			}
			return RenderOTel(document)
		}},
		{"logging", func() ([]byte, error) {
			document, err := DecodeLogging(logging)
			if err != nil {
				return nil, err
			}
			return RenderLogging(document)
		}},
	}

	for _, test := range renderers {
		t.Run(test.name, func(t *testing.T) {
			first, err := test.render()
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for iteration := 0; iteration < 10; iteration++ {
				rendered, err := test.render()
				if err != nil {
					t.Fatalf("render iteration %d: %v", iteration, err)
				}
				if !bytes.Equal(first, rendered) {
					t.Fatalf("render is not deterministic at iteration %d", iteration)
				}
			}
		})
	}
}

func TestRenderRoundTripIsStable(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		decode  func([]byte) (interface{ Validate() []*ValidationError }, error)
		render  func(interface{}) ([]byte, error)
	}{
		{
			name:    "metrics",
			fixture: "valid/metrics.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeMetrics(data)
			},
			render: func(document interface{}) ([]byte, error) {
				return RenderMetrics(document.(*MetricsDocument))
			},
		},
		{
			name:    "otel",
			fixture: "valid/otel.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeOTel(data)
			},
			render: func(document interface{}) ([]byte, error) {
				return RenderOTel(document.(*OTelDocument))
			},
		},
		{
			name:    "logging",
			fixture: "valid/logging.yaml",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeLogging(data)
			},
			render: func(document interface{}) ([]byte, error) {
				return RenderLogging(document.(*LoggingDocument))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := test.decode(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			first, err := test.render(document)
			if err != nil {
				t.Fatalf("first render: %v", err)
			}
			reparsed, err := test.decode(first)
			if err != nil {
				t.Fatalf("re-decode rendered output: %v", err)
			}
			if violations := reparsed.Validate(); len(violations) > 0 {
				t.Fatalf("rendered output is not valid: %v", violations)
			}
			second, err := test.render(reparsed)
			if err != nil {
				t.Fatalf("second render: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("render -> decode -> render is not stable")
			}
		})
	}
}

func TestRenderRejectsInvalidDocument(t *testing.T) {
	document, err := DecodeMetrics(readFixture(t, "invalid/duplicate-id.yaml"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := RenderMetrics(document); err == nil {
		t.Fatal("expected render to reject invalid document")
	}
}

func TestRenderSortsByStableID(t *testing.T) {
	document := &MetricsDocument{
		SchemaVersion: SchemaVersionMetrics,
		DocumentType:  DocumentTypeMetrics,
		Source:        Source{IRSchemaVersion: "v1", ServiceName: "orders"},
		GeneratedBy:   GeneratedBy{Name: "si", Version: "v0.2.0"},
		Metrics: []Metric{
			metricFixture("metric:z", "z_requests_total"),
			metricFixture("metric:a", "a_requests_total"),
			metricFixture("metric:m", "m_requests_total"),
		},
	}
	rendered, err := RenderMetrics(document)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	reparsed, err := DecodeMetrics(rendered)
	if err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	ids := []string{reparsed.Metrics[0].ID, reparsed.Metrics[1].ID, reparsed.Metrics[2].ID}
	if ids[0] != "metric:a" || ids[1] != "metric:m" || ids[2] != "metric:z" {
		t.Fatalf("expected metrics sorted by stable ID, got %v", ids)
	}
}

func TestRenderSortsAttributesByKey(t *testing.T) {
	document := &OTelDocument{
		SchemaVersion:              SchemaVersionOTel,
		DocumentType:               DocumentTypeOTel,
		PlanKind:                   OTelPlanKind,
		SemanticConventionsVersion: OTelSemanticConventionsVersion,
		Source:                     Source{IRSchemaVersion: "v1", ServiceName: "orders"},
		GeneratedBy:                GeneratedBy{Name: "si", Version: "v0.2.0"},
		Spans: []Span{{
			ID:        "span:a",
			Name:      "GET /orders",
			Kind:      SpanKindServer,
			Target:    TargetRef{Type: TargetKindEndpoint, ID: "endpoint:a"},
			Lifecycle: Lifecycle{Start: TriggerStart, End: TriggerEnd},
			Parent:    Parent{Strategy: ParentNewRoot},
			Attributes: []Attribute{
				{Key: "zebra", Binding: ValueBinding{Source: ValueSourceConstant, String: "z"}},
				{Key: "apple", Binding: ValueBinding{Source: ValueSourceConstant, String: "a"}},
			},
		}},
	}
	rendered, err := RenderOTel(document)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	reparsed, err := DecodeOTel(rendered)
	if err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if reparsed.Spans[0].Attributes[0].Key != "apple" || reparsed.Spans[0].Attributes[1].Key != "zebra" {
		t.Fatalf("expected attributes sorted by key")
	}
}

func TestRenderDoesNotMutateInput(t *testing.T) {
	document, err := DecodeMetrics(readFixture(t, "valid/metrics.yaml"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	before, err := RenderMetrics(document)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Rendering again must not reorder the caller's document.
	after, err := RenderMetrics(document)
	if err != nil {
		t.Fatalf("render again: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("repeated render of the same model diverged")
	}
}

func TestRenderOutputHasNoAnchorsAliasesOrTimestamps(t *testing.T) {
	document, err := DecodeOTel(readFixture(t, "valid/otel.yaml"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	rendered, err := RenderOTel(document)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(rendered)
	for _, forbidden := range []string{"&", "*", "!!timestamp"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("rendered output contains forbidden marker %q", forbidden)
		}
	}
}

func metricFixture(id, name string) Metric {
	return Metric{
		ID:     id,
		Name:   name,
		Type:   MetricTypeCounter,
		Target: TargetRef{Type: TargetKindEndpoint, ID: "endpoint:" + name},
		Record: Record{
			Trigger: TriggerEnd,
			Value:   ValueBinding{Source: ValueSourceConstant, Number: 1},
		},
	}
}

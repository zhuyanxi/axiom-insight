package generator

import (
	"strings"
	"testing"
)

func TestValidateMetricsRejects(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantField string
	}{
		{"duplicate ID", "invalid/duplicate-id.yaml", "metrics[1].id"},
		{"unordered buckets", "invalid/unordered-buckets.yaml", "metrics[0].buckets"},
		{"duplicate quantile", "invalid/duplicate-quantile.yaml", "metrics[0].quantiles"},
		{"empty value binding", "invalid/empty-value-binding.yaml", "metrics[0].record.value.source"},
		{"bad unit", "invalid/bad-unit.yaml", "metrics[0].unit"},
		{"counter with buckets", "invalid/counter-with-buckets.yaml", "metrics[0].buckets"},
		{"unknown enum", "invalid/unknown-enum.yaml", "metrics[0].type"},
		{"wrong schema version", "invalid/wrong-schema-version.yaml", "schema_version"},
		{"wrong document type", "invalid/wrong-document-type.yaml", "document_type"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := DecodeMetrics(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("fixture must decode (semantic violation expected): %v", err)
			}
			violations := document.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected semantic violations, got none")
			}
			if !hasViolationAt(violations, test.wantField) {
				t.Fatalf("expected violation at %q, got: %v", test.wantField, violations)
			}
		})
	}
}

func TestValidateOTelRejects(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantField string
	}{
		{"new_root with carrier", "invalid/bad-parent-combo.yaml", "spans[0].parent.carrier"},
		{"dangling static parent", "invalid/dangling-static-parent.yaml", "spans[0].parent.static_parent_span_id"},
		{"incomplete status mapping", "invalid/bad-status-mapping.yaml", "spans[0].status.mapping"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := DecodeOTel(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("fixture must decode (semantic violation expected): %v", err)
			}
			violations := document.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected semantic violations, got none")
			}
			if !hasViolationAt(violations, test.wantField) {
				t.Fatalf("expected violation at %q, got: %v", test.wantField, violations)
			}
		})
	}
}

func TestValidateLoggingRejects(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantField string
	}{
		{"redaction not immutable", "invalid/redaction-not-immutable.yaml", "redaction.immutable"},
		{"duplicate normalized field key", "invalid/duplicate-field-key.yaml", "events[0].fields[1].key"},
		{"trace_id wrong source", "invalid/correlation-wrong-source.yaml", "events[0].fields[0].binding.source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := DecodeLogging(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("fixture must decode (semantic violation expected): %v", err)
			}
			violations := document.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected semantic violations, got none")
			}
			if !hasViolationAt(violations, test.wantField) {
				t.Fatalf("expected violation at %q, got: %v", test.wantField, violations)
			}
		})
	}
}

func TestValidateHeaderRejectsEmptyRequiredFields(t *testing.T) {
	document := &MetricsDocument{
		SchemaVersion: SchemaVersionMetrics,
		DocumentType:  DocumentTypeMetrics,
		Source:        Source{IRSchemaVersion: "v1"},
		GeneratedBy:   GeneratedBy{Name: "si", Version: "v0.2.0"},
	}
	violations := document.Validate()
	if !hasViolationAt(violations, "source.service_name") {
		t.Fatalf("expected empty service name violation, got: %v", violations)
	}
}

func TestValidateMetricsRejectsConstantWithoutValue(t *testing.T) {
	document := &MetricsDocument{
		SchemaVersion: SchemaVersionMetrics,
		DocumentType:  DocumentTypeMetrics,
		Source:        Source{IRSchemaVersion: "v1", ServiceName: "orders"},
		GeneratedBy:   GeneratedBy{Name: "si", Version: "v0.2.0"},
		Metrics: []Metric{{
			ID:     "metric:a",
			Name:   "a_requests_total",
			Type:   MetricTypeCounter,
			Target: TargetRef{Type: TargetKindEndpoint, ID: "endpoint:a"},
			Record: Record{Trigger: TriggerEnd, Value: ValueBinding{Source: ValueSourceConstant}},
		}},
	}
	violations := document.Validate()
	if !anyViolation(violations, "metrics[0].record.value") {
		t.Fatalf("expected constant-without-value violation, got: %v", violations)
	}
}

func TestValidateOTelRejectsStatusSettingOutOfVocabulary(t *testing.T) {
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
			Status: &StatusPolicy{Mapping: map[string]string{
				RuntimeStatusOK:        StatusUnset,
				RuntimeStatusError:     "exploded",
				RuntimeStatusCancelled: StatusError,
				RuntimeStatusTimeout:   StatusError,
				RuntimeStatusUnknown:   StatusUnset,
			}},
		}},
	}
	violations := document.Validate()
	if !anyViolation(violations, "spans[0].status.mapping.error") {
		t.Fatalf("expected out-of-vocabulary status setting violation, got: %v", violations)
	}
}

func TestValidateLoggingRejectsDuplicateEventNamePerTarget(t *testing.T) {
	// The same event name across different targets is meaningful (every
	// dependency emits "dependency.operation.failed"); a duplicate name
	// for the SAME target is rejected.
	build := func(targetID string) LogEvent {
		return LogEvent{
			ID:        "log:" + targetID,
			EventName: "dependency.operation.failed",
			Target:    TargetRef{Type: TargetKindDependency, ID: targetID},
			Trigger:   TriggerEnd,
			Condition: Condition{StatusIn: []string{RuntimeStatusError}},
			Severity:  Severity{Constant: LogSeverityError},
			Fields: []Field{{
				Key:     "status",
				Type:    ValueTypeStatus,
				Binding: ValueBinding{Source: ValueSourceRuntimeResult, Path: "operation.status"},
			}},
		}
	}
	document := &LoggingDocument{
		SchemaVersion: SchemaVersionLogging,
		DocumentType:  DocumentTypeLogging,
		Source:        Source{IRSchemaVersion: "v1", ServiceName: "orders"},
		GeneratedBy:   GeneratedBy{Name: "si", Version: "v0.2.0"},
		Redaction:     Redaction{Immutable: true, FieldNames: []string{"authorization"}},
		Events: []LogEvent{
			build("dependency:a"),
			build("dependency:b"),
		},
	}
	if violations := document.Validate(); len(violations) > 0 {
		t.Fatalf("same event name across different targets must be valid, got: %v", violations)
	}

	document.Events = []LogEvent{build("dependency:a"), build("dependency:a")}
	violations := document.Validate()
	if !hasViolationAt(violations, "events[1].event_name") {
		t.Fatalf("expected duplicate event name violation for the same target, got: %v", violations)
	}
}

// TestValidateLoggingAllowsOptionalTraceID: trace_id may be optional when
// no Root Span context is provable (P1-12 dependency events); the source
// must still be runtime_context.
func TestValidateLoggingAllowsOptionalTraceID(t *testing.T) {
	document := &LoggingDocument{
		SchemaVersion: SchemaVersionLogging,
		DocumentType:  DocumentTypeLogging,
		Source:        Source{IRSchemaVersion: "v1", ServiceName: "orders"},
		GeneratedBy:   GeneratedBy{Name: "si", Version: "v0.2.0"},
		Redaction:     Redaction{Immutable: true, FieldNames: []string{"authorization"}},
		Events: []LogEvent{{
			ID:        "log:a",
			EventName: "dependency.operation.failed",
			Target:    TargetRef{Type: TargetKindDependency, ID: "dependency:a"},
			Trigger:   TriggerEnd,
			Condition: Condition{StatusIn: []string{RuntimeStatusError}},
			Severity:  Severity{Constant: LogSeverityError},
			Fields: []Field{{
				Key:     "trace_id",
				Type:    ValueTypeString,
				Binding: ValueBinding{Source: ValueSourceRuntimeContext, Path: "trace.id"},
			}},
		}},
	}
	violations := document.Validate()
	if hasViolationAt(violations, "events[0].fields[0].required") {
		t.Fatalf("optional trace_id must be allowed, got: %v", violations)
	}
}

func TestValidationErrorFormatting(t *testing.T) {
	violation := &ValidationError{Document: "metrics", Field: "metrics[0].id", Message: "duplicate metric ID"}
	text := violation.Error()
	if !strings.Contains(text, "metrics") || !strings.Contains(text, "metrics[0].id") || !strings.Contains(text, "duplicate metric ID") {
		t.Fatalf("unexpected error format: %s", text)
	}
}

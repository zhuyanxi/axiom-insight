package generator

import (
	"bytes"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// RenderMetrics deterministically serializes a valid metrics.yaml document.
// The document is validated first; invalid documents never render. Lists are
// copied and sorted by stable ID before encoding, so the caller's document
// is never mutated. Output is LF, UTF-8, with no anchors, aliases or
// timestamps.
func RenderMetrics(document *MetricsDocument) ([]byte, error) {
	if violations := ValidateMetrics(document); len(violations) > 0 {
		return nil, fmt.Errorf("cannot render invalid metrics document: %w", violations[0])
	}
	clone := *document
	clone.Metrics = cloneSortedMetrics(document.Metrics)
	return encodeYAML(&clone)
}

// RenderOTel deterministically serializes a valid otel.yaml document.
func RenderOTel(document *OTelDocument) ([]byte, error) {
	if violations := ValidateOTel(document); len(violations) > 0 {
		return nil, fmt.Errorf("cannot render invalid otel document: %w", violations[0])
	}
	clone := *document
	clone.Resources = cloneSortedAttributes(document.Resources)
	clone.Spans = cloneSortedSpans(document.Spans)
	return encodeYAML(&clone)
}

// RenderLogging deterministically serializes a valid logging.yaml document.
func RenderLogging(document *LoggingDocument) ([]byte, error) {
	if violations := ValidateLogging(document); len(violations) > 0 {
		return nil, fmt.Errorf("cannot render invalid logging document: %w", violations[0])
	}
	clone := *document
	clone.Events = cloneSortedLogEvents(document.Events)
	return encodeYAML(&clone)
}

// encodeYAML encodes through a yaml.v3 Encoder with two-space indentation.
// Map keys (status mappings) are emitted sorted by yaml.v3, so the output is
// deterministic for identical inputs.
func encodeYAML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close YAML encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

func cloneSortedMetrics(metrics []Metric) []Metric {
	cloned := make([]Metric, len(metrics))
	for index, metric := range metrics {
		metric.Attributes = cloneSortedAttributes(metric.Attributes)
		metric.Buckets = append([]float64(nil), metric.Buckets...)
		metric.Quantiles = append([]float64(nil), metric.Quantiles...)
		cloned[index] = metric
	}
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].ID < cloned[right].ID })
	return cloned
}

func cloneSortedSpans(spans []Span) []Span {
	cloned := make([]Span, len(spans))
	for index, span := range spans {
		span.Attributes = cloneSortedAttributes(span.Attributes)
		if span.Status != nil {
			mapping := make(map[string]string, len(span.Status.Mapping))
			for key, value := range span.Status.Mapping {
				mapping[key] = value
			}
			span.Status = &StatusPolicy{Mapping: mapping}
		}
		span.Events = cloneSortedSpanEvents(span.Events)
		cloned[index] = span
	}
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].ID < cloned[right].ID })
	return cloned
}

func cloneSortedLogEvents(events []LogEvent) []LogEvent {
	cloned := make([]LogEvent, len(events))
	for index, event := range events {
		event.Fields = cloneSortedFields(event.Fields)
		event.Condition.StatusIn = append([]string(nil), event.Condition.StatusIn...)
		cloned[index] = event
	}
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].ID < cloned[right].ID })
	return cloned
}

func cloneSortedAttributes(attributes []Attribute) []Attribute {
	cloned := make([]Attribute, len(attributes))
	copy(cloned, attributes)
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].Key < cloned[right].Key })
	return cloned
}

func cloneSortedFields(fields []Field) []Field {
	cloned := make([]Field, len(fields))
	copy(cloned, fields)
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].Key < cloned[right].Key })
	return cloned
}

func cloneSortedSpanEvents(events []SpanEvent) []SpanEvent {
	cloned := make([]SpanEvent, len(events))
	for index, event := range events {
		event.Statuses = append([]string(nil), event.Statuses...)
		event.Attributes = cloneSortedAttributes(event.Attributes)
		cloned[index] = event
	}
	sort.SliceStable(cloned, func(left, right int) bool { return cloned[left].ID < cloned[right].ID })
	return cloned
}

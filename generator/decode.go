package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeMetrics strictly parses data as a metrics.yaml document.
func DecodeMetrics(data []byte) (*MetricsDocument, error) {
	document := new(MetricsDocument)
	if err := decodeStrict(SchemaVersionMetrics, data, document); err != nil {
		return nil, err
	}
	return document, nil
}

// DecodeOTel strictly parses data as an otel.yaml document.
func DecodeOTel(data []byte) (*OTelDocument, error) {
	document := new(OTelDocument)
	if err := decodeStrict(SchemaVersionOTel, data, document); err != nil {
		return nil, err
	}
	return document, nil
}

// DecodeLogging strictly parses data as a logging.yaml document.
func DecodeLogging(data []byte) (*LoggingDocument, error) {
	document := new(LoggingDocument)
	if err := decodeStrict(SchemaVersionLogging, data, document); err != nil {
		return nil, err
	}
	return document, nil
}

// decodeStrict rejects, at parse time, everything the closed contract
// forbids: multiple YAML documents, duplicate mapping keys, aliases,
// anchors, timestamps, non-finite numbers, non-string mapping keys, unknown
// fields and wrong scalar types. It parses to a yaml.Node, converts that
// node to a strictly typed generic value and then decodes through
// encoding/json with DisallowUnknownFields so type mismatches and unknown
// fields become errors instead of silent coercion.
func decodeStrict(documentName string, data []byte, out any) error {
	reader := bytes.NewReader(data)
	yamlDecoder := yaml.NewDecoder(reader)
	var node yaml.Node
	if err := yamlDecoder.Decode(&node); err != nil {
		return fmt.Errorf("%s: invalid YAML: %w", documentName, err)
	}
	var extra yaml.Node
	if err := yamlDecoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: multiple YAML documents are not allowed", documentName)
	}

	value, err := nodeToValue(&node, documentName, "$")
	if err != nil {
		return err
	}
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%s: internal conversion failed: %w", documentName, err)
	}

	jsonDecoder := json.NewDecoder(bytes.NewReader(jsonData))
	jsonDecoder.DisallowUnknownFields()
	if err := jsonDecoder.Decode(out); err != nil {
		return fmt.Errorf("%s: %w", documentName, err)
	}
	return nil
}

// nodeToValue converts a yaml.Node tree into a strictly typed generic value
// (string, int64, float64, bool, nil, map[string]any, []any) while rejecting
// aliases, anchors, timestamps, non-finite floats, non-string mapping keys
// and duplicate mapping keys. Plain scalars resolve through the YAML core
// schema so `123` stays a number and "123" stays a string.
func nodeToValue(node *yaml.Node, documentName, path string) (any, error) {
	fail := func(format string, args ...any) (any, error) {
		return nil, fmt.Errorf("%s: %s: %s", documentName, path, fmt.Sprintf(format, args...))
	}

	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return fail("document node must have exactly one child")
		}
		return nodeToValue(node.Content[0], documentName, path)

	case yaml.MappingNode:
		if node.Anchor != "" {
			return fail("anchors are not allowed")
		}
		result := make(map[string]any, len(node.Content)/2)
		seen := make(map[string]bool, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			valueNode := node.Content[index+1]
			if keyNode.Kind != yaml.ScalarNode {
				return fail("mapping keys must be scalars")
			}
			if keyNode.Tag != "!!str" {
				return fail("mapping key %q must be a string, got %s tag", keyNode.Value, keyNode.Tag)
			}
			key := keyNode.Value
			if seen[key] {
				return fail("duplicate key %q", key)
			}
			seen[key] = true
			childPath := path + "." + key
			child, err := nodeToValue(valueNode, documentName, childPath)
			if err != nil {
				return nil, err
			}
			result[key] = child
		}
		return result, nil

	case yaml.SequenceNode:
		if node.Anchor != "" {
			return fail("anchors are not allowed")
		}
		sequence := make([]any, 0, len(node.Content))
		for index, item := range node.Content {
			child, err := nodeToValue(item, documentName, fmt.Sprintf("%s[%d]", path, index))
			if err != nil {
				return nil, err
			}
			sequence = append(sequence, child)
		}
		return sequence, nil

	case yaml.AliasNode:
		return fail("aliases are not allowed")

	case yaml.ScalarNode:
		if node.Anchor != "" {
			return fail("anchors are not allowed")
		}
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!int":
			value, err := strconv.ParseInt(node.Value, 0, 64)
			if err != nil {
				return fail("invalid integer %q", node.Value)
			}
			return value, nil
		case "!!float":
			value, err := strconv.ParseFloat(strings.ToLower(node.Value), 64)
			if err != nil {
				// YAML spells the special floats as .nan/.inf, which Go's
				// parser renders as NaN/Inf.
				if strings.Contains(strings.ToLower(node.Value), "nan") || strings.Contains(strings.ToLower(node.Value), "inf") {
					return fail("non-finite number %q is not allowed", node.Value)
				}
				return fail("invalid number %q", node.Value)
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fail("non-finite number %q is not allowed", node.Value)
			}
			return value, nil
		case "!!bool":
			value, err := strconv.ParseBool(node.Value)
			if err != nil {
				return fail("invalid boolean %q", node.Value)
			}
			return value, nil
		case "!!null":
			return nil, nil
		case "!!timestamp":
			return fail("timestamps are not allowed")
		default:
			return fail("unsupported scalar tag %q", node.Tag)
		}

	default:
		return fail("unsupported node kind %d", node.Kind)
	}
}

package policy

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

// DecodeConfigFile strictly extracts and decodes the `generation` node
// from full si.yaml contents. It returns nil when the file has no
// generation node. Unknown fields inside the generation node, duplicate
// generation keys, aliases, anchors, timestamps and non-finite numbers are
// rejected. Other si.yaml nodes keep the Phase 0 loader behavior.
func DecodeConfigFile(data []byte) (*GenerationConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	yamlDecoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := yamlDecoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("si.yaml: invalid YAML: %w", err)
	}

	root := &document
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) != 1 {
			return nil, fmt.Errorf("si.yaml: document node must have exactly one child")
		}
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil
	}

	var generationNode *yaml.Node
	for index := 0; index+1 < len(root.Content); index += 2 {
		keyNode := root.Content[index]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Tag == "!!str" && keyNode.Value == "generation" {
			if generationNode != nil {
				return nil, fmt.Errorf("si.yaml: duplicate key %q", "generation")
			}
			generationNode = root.Content[index+1]
		}
	}
	if generationNode == nil {
		return nil, nil
	}

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(generationNode); err != nil {
		return nil, fmt.Errorf("si.yaml: re-encode generation node: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("si.yaml: re-encode generation node: %w", err)
	}
	return DecodeGeneration(buffer.Bytes())
}

// DecodeGeneration strictly parses the generation node as YAML. The same
// closed rules as the generated-document decoder apply: exactly one
// document, no duplicate mapping keys, no aliases or anchors, no
// timestamps, no non-finite numbers, string mapping keys only, correct
// scalar types and no unknown fields. Error paths read
// "generation.<field>[<index>]".
func DecodeGeneration(data []byte) (*GenerationConfig, error) {
	reader := bytes.NewReader(data)
	yamlDecoder := yaml.NewDecoder(reader)
	var node yaml.Node
	if err := yamlDecoder.Decode(&node); err != nil {
		return nil, fmt.Errorf("generation: invalid YAML: %w", err)
	}
	var extra yaml.Node
	if err := yamlDecoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("generation: multiple YAML documents are not allowed")
	}

	value, err := nodeToValue(&node, "generation", "generation")
	if err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("generation: internal conversion failed: %w", err)
	}
	jsonDecoder := json.NewDecoder(bytes.NewReader(jsonData))
	jsonDecoder.DisallowUnknownFields()
	config := new(GenerationConfig)
	if err := jsonDecoder.Decode(config); err != nil {
		return nil, fmt.Errorf("generation: %w", err)
	}
	return config, nil
}

// nodeToValue converts a yaml.Node tree into a strictly typed generic value
// (string, int64, float64, bool, nil, map[string]any, []any) while
// rejecting aliases, anchors, timestamps, non-finite floats, non-string
// mapping keys and duplicate mapping keys. Plain scalars resolve through
// the YAML core schema so `123` stays a number and "123" stays a string.
// The root path is "generation" so every error path matches the config
// contract, e.g. "generation.metrics.histogram_buckets_seconds[2]".
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

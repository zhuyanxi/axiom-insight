package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
	"gopkg.in/yaml.v3"
)

// TestGoldenSchemasCompatible: every committed golden file validates
// against its v1 machine schema and decodes strictly (task 5 of P1-16).
func TestGoldenSchemasCompatible(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "generator", "golden")
	scenarios, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read golden root: %v", err)
	}
	checked := 0
	for _, scenario := range scenarios {
		if !scenario.IsDir() {
			continue
		}
		scenarioDir := filepath.Join(root, scenario.Name())
		for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
			path := filepath.Join(scenarioDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read golden %s: %v", path, err)
				continue
			}
			checked++
			schema, err := os.ReadFile(filepath.Join("..", "..", "schemas", "generator", "v1", schemaFor(name)))
			if err != nil {
				t.Fatalf("read schema for %s: %v", name, err)
			}
			jsonData := goldenToJSON(t, data)
			if err := schemacheck.Validate(schema, jsonData); err != nil {
				t.Errorf("golden %s fails its v1 schema: %v", path, err)
			}
			if _, err := decodeDoc(name, data); err != nil {
				t.Errorf("golden %s does not decode strictly: %v", path, err)
			}
		}
	}
	if checked < 12 {
		t.Fatalf("checked %d golden files, want at least 12", checked)
	}
}

func schemaFor(name string) string {
	return strings.TrimSuffix(name, ".yaml") + ".schema.json"
}

// goldenToJSON converts a golden YAML to JSON for schema validation.
func goldenToJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		t.Fatalf("parse golden YAML: %v", err)
	}
	value, err := yamlNodeToValue(&node, "$")
	if err != nil {
		t.Fatalf("convert golden YAML: %v", err)
	}
	contents, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal golden JSON: %v", err)
	}
	return contents
}

// yamlNodeToValue converts a YAML node to a generic JSON value,
// rejecting non-string mapping keys and non-finite numbers.
func yamlNodeToValue(node *yaml.Node, path string) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, fmt.Errorf("document node must have exactly one child")
		}
		return yamlNodeToValue(node.Content[0], path)
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index].Value
			child, err := yamlNodeToValue(node.Content[index+1], path+"."+key)
			if err != nil {
				return nil, err
			}
			result[key] = child
		}
		return result, nil
	case yaml.SequenceNode:
		sequence := make([]any, 0, len(node.Content))
		for index, item := range node.Content {
			child, err := yamlNodeToValue(item, path+"["+itoa(index)+"]")
			if err != nil {
				return nil, err
			}
			sequence = append(sequence, child)
		}
		return sequence, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str":
			return node.Value, nil
		case "!!bool":
			return node.Value == "true", nil
		case "!!int":
			var value int64
			if _, err := fmt.Sscanf(node.Value, "%d", &value); err != nil {
				return nil, err
			}
			return value, nil
		case "!!float":
			var value float64
			if _, err := fmt.Sscanf(node.Value, "%g", &value); err != nil {
				return nil, err
			}
			return value, nil
		case "!!null":
			return nil, nil
		}
		return node.Value, nil
	default:
		return nil, nil
	}
}

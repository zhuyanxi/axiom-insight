package dashboard

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DecodeDashboardConfigFile strictly extracts and decodes the `dashboard`
// node from full si.yaml contents. It returns nil when the file has no
// dashboard node. Unknown fields inside the dashboard node, duplicate
// dashboard keys, aliases, anchors, timestamps and non-finite numbers are
// rejected. Other si.yaml nodes keep the Phase 0 loader behavior. The
// file is limited to MaxConfigBytes.
func DecodeDashboardConfigFile(data []byte) (*DashboardConfig, error) {
	if len(data) > MaxConfigBytes {
		return nil, fmt.Errorf("si.yaml: file exceeds the %d-byte configuration limit", MaxConfigBytes)
	}
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

	var dashboardNode *yaml.Node
	for index := 0; index+1 < len(root.Content); index += 2 {
		keyNode := root.Content[index]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Tag == "!!str" && keyNode.Value == "dashboard" {
			if dashboardNode != nil {
				return nil, fmt.Errorf("si.yaml: duplicate key %q", "dashboard")
			}
			dashboardNode = root.Content[index+1]
		}
	}
	if dashboardNode == nil {
		return nil, nil
	}

	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(dashboardNode); err != nil {
		return nil, fmt.Errorf("si.yaml: re-encode dashboard node: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("si.yaml: re-encode dashboard node: %w", err)
	}
	return DecodeDashboard(buffer.Bytes())
}

// DecodeDashboard strictly parses the dashboard node as YAML. The same
// closed rules as the generated-document decoder apply: exactly one
// document, no duplicate mapping keys, no aliases or anchors, no
// timestamps, no non-finite numbers, string mapping keys only, correct
// scalar types and no unknown fields. Error paths read
// "dashboard.<field>" and never echo rejected values.
func DecodeDashboard(data []byte) (*DashboardConfig, error) {
	reader := bytes.NewReader(data)
	yamlDecoder := yaml.NewDecoder(reader)
	var node yaml.Node
	if err := yamlDecoder.Decode(&node); err != nil {
		return nil, fmt.Errorf("dashboard: invalid YAML: %w", err)
	}
	var extra yaml.Node
	if err := yamlDecoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("dashboard: multiple YAML documents are not allowed")
	}

	value, err := dashboardNodeToValue(&node, "dashboard")
	if err != nil {
		return nil, err
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		if value == nil {
			// An explicit `dashboard: ~` node is an empty configuration.
			return new(DashboardConfig), nil
		}
		return nil, fmt.Errorf("dashboard: root must be a mapping")
	}
	return strictDecodeDashboard(mapping)
}

// strictDecodeDashboard maps the generic dashboard node onto the typed
// DashboardConfig, rejecting unknown fields and wrong scalar types with
// exact "dashboard.<field>" paths. Messages never echo the rejected value.
func strictDecodeDashboard(mapping map[string]any) (*DashboardConfig, error) {
	config := new(DashboardConfig)
	for key, value := range mapping {
		path := "dashboard." + key
		var err error
		switch key {
		case "output_dir":
			err = decodeStringField(path, value, &config.OutputDir)
		case "title_suffix":
			err = decodeStringField(path, value, &config.TitleSuffix)
		case "datasource_variable_name":
			err = decodeStringField(path, value, &config.DatasourceVariableName)
		case "rate_interval":
			err = decodeStringField(path, value, &config.RateInterval)
		case "timezone":
			err = decodeStringField(path, value, &config.Timezone)
		case "refresh":
			err = decodeStringField(path, value, &config.Refresh)
		case "include_trace_links":
			err = decodeBoolField(path, value, &config.IncludeTraceLinks)
		case "include_client_dependencies":
			err = decodeBoolField(path, value, &config.IncludeClientDependencies)
		case "strict":
			err = decodeBoolField(path, value, &config.Strict)
		case "max_panels":
			err = decodeIntField(path, value, &config.MaxPanels)
		case "max_queries":
			err = decodeIntField(path, value, &config.MaxQueries)
		default:
			return nil, fmt.Errorf("dashboard: %s: unknown field", path)
		}
		if err != nil {
			return nil, err
		}
	}
	return config, nil
}

func decodeStringField(path string, value any, target **string) error {
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("dashboard: %s: must be a string", path)
	}
	*target = &text
	return nil
}

func decodeBoolField(path string, value any, target **bool) error {
	boolean, ok := value.(bool)
	if !ok {
		return fmt.Errorf("dashboard: %s: must be a boolean", path)
	}
	*target = &boolean
	return nil
}

func decodeIntField(path string, value any, target **int64) error {
	integer, ok := value.(int64)
	if !ok {
		return fmt.Errorf("dashboard: %s: must be an integer", path)
	}
	*target = &integer
	return nil
}

// dashboardNodeToValue converts a yaml.Node tree into a strictly typed
// generic value (string, int64, float64, bool, nil, map[string]any,
// []any) while rejecting aliases, anchors, timestamps, non-finite floats,
// non-string mapping keys and duplicate mapping keys. Plain scalars
// resolve through the YAML core schema so `123` stays a number and "123"
// stays a string. Error paths read "dashboard.<field>"; error messages
// never echo the rejected scalar value, which could carry a secret.
func dashboardNodeToValue(node *yaml.Node, path string) (any, error) {
	fail := func(format string, args ...any) (any, error) {
		return nil, fmt.Errorf("%s: %s: %s", "dashboard", path, fmt.Sprintf(format, args...))
	}

	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return fail("document node must have exactly one child")
		}
		return dashboardNodeToValue(node.Content[0], path)

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
				// The key is a path component, not a rejected value; its
				// name is safe to include in the path.
				return nil, fmt.Errorf("dashboard: %s.%s: mapping key must be a string", path, keyNode.Value)
			}
			key := keyNode.Value
			childPath := path + "." + key
			if seen[key] {
				return nil, fmt.Errorf("dashboard: %s: duplicate key %q", childPath, key)
			}
			seen[key] = true
			child, err := dashboardNodeToValue(valueNode, childPath)
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
			child, err := dashboardNodeToValue(item, fmt.Sprintf("%s[%d]", path, index))
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
				return fail("invalid integer")
			}
			return value, nil
		case "!!float":
			value, err := strconv.ParseFloat(strings.ToLower(node.Value), 64)
			if err != nil {
				if strings.Contains(strings.ToLower(node.Value), "nan") || strings.Contains(strings.ToLower(node.Value), "inf") {
					return fail("non-finite number is not allowed")
				}
				return fail("invalid number")
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fail("non-finite number is not allowed")
			}
			return value, nil
		case "!!bool":
			value, err := strconv.ParseBool(node.Value)
			if err != nil {
				return fail("invalid boolean")
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

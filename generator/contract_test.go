package generator

import (
	"encoding/json"
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// allowedKeywords is the JSON Schema keyword subset the generator schemas
// may use. The subset validator ignores everything else, so a schema using
// an unsupported keyword could silently pass invalid documents; the audit
// test below prevents that.
var allowedKeywords = map[string]bool{
	"$schema": true, "$id": true, "title": true, "description": true,
	"type": true, "properties": true, "additionalProperties": true,
	"required": true, "items": true, "enum": true, "const": true,
	"minItems": true, "uniqueItems": true, "minLength": true, "pattern": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true,
	"exclusiveMaximum": true, "oneOf": true, "anyOf": true, "allOf": true,
	"$defs": true, "$ref": true, "default": true,
}

func yamlToJSON(t *testing.T, data []byte) ([]byte, error) {
	t.Helper()
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	value, err := nodeToValue(&node, "contract", "$")
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func loadSchema(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(schemaPath(name))
	if err != nil {
		t.Fatalf("read schema %s: %v", name, err)
	}
	return data
}

func TestSchemasUseOnlySupportedKeywords(t *testing.T) {
	for _, name := range []string{"metrics.schema.json", "otel.schema.json", "logging.schema.json"} {
		t.Run(name, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal(loadSchema(t, name), &schema); err != nil {
				t.Fatalf("parse schema: %v", err)
			}
			var walk func(node any)
			walk = func(node any) {
				switch value := node.(type) {
				case map[string]any:
					for key, child := range value {
						switch key {
						case "properties", "$defs":
							// Keys here are property or definition names, not
							// keywords; walk only their schema values.
							if mapping, ok := child.(map[string]any); ok {
								for _, schema := range mapping {
									walk(schema)
								}
							}
						default:
							if !allowedKeywords[key] {
								t.Errorf("schema uses unsupported keyword %q", key)
							}
							walk(child)
						}
					}
				case []any:
					for _, child := range value {
						walk(child)
					}
				}
			}
			walk(schema)
		})
	}
}

func TestContractValidFixtures(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		schema   string
		validate func([]byte) []*ValidationError
	}{
		{
			name:    "metrics",
			fixture: "valid/metrics.yaml",
			schema:  "metrics.schema.json",
			validate: func(data []byte) []*ValidationError {
				document, err := DecodeMetrics(data)
				if err != nil {
					return []*ValidationError{{Document: "metrics", Field: "$", Message: err.Error()}}
				}
				return document.Validate()
			},
		},
		{
			name:    "otel",
			fixture: "valid/otel.yaml",
			schema:  "otel.schema.json",
			validate: func(data []byte) []*ValidationError {
				document, err := DecodeOTel(data)
				if err != nil {
					return []*ValidationError{{Document: "otel", Field: "$", Message: err.Error()}}
				}
				return document.Validate()
			},
		},
		{
			name:    "logging",
			fixture: "valid/logging.yaml",
			schema:  "logging.schema.json",
			validate: func(data []byte) []*ValidationError {
				document, err := DecodeLogging(data)
				if err != nil {
					return []*ValidationError{{Document: "logging", Field: "$", Message: err.Error()}}
				}
				return document.Validate()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readFixture(t, test.fixture)
			if violations := test.validate(data); len(violations) > 0 {
				t.Fatalf("Go validator rejected a valid fixture: %v", violations)
			}
			jsonData, err := yamlToJSON(t, data)
			if err != nil {
				t.Fatalf("convert valid fixture to JSON: %v", err)
			}
			if err := schemacheck.Validate(loadSchema(t, test.schema), jsonData); err != nil {
				t.Fatalf("JSON Schema rejected a valid fixture: %v", err)
			}
		})
	}
}

func TestContractSchemaInvalidFixtures(t *testing.T) {
	// Only fixtures whose invalidity is expressible in JSON Schema belong
	// here: unknown fields, wrong scalar types, unknown enums, non-finite
	// numbers, aliases, missing required fields, duplicate keys, and
	// version/type mismatches. Semantic-only rules (duplicate IDs, bucket
	// ordering, parent/status rules, redaction rules) are covered by the Go
	// validators in validate_test.go.
	tests := []struct {
		name    string
		fixture string
		schema  string
		decode  func([]byte) error
	}{
		{
			name:    "unknown-field",
			fixture: "invalid/unknown-field.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "bad-scalar-type",
			fixture: "invalid/bad-scalar-type.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "unknown-enum",
			fixture: "invalid/unknown-enum.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "non-finite",
			fixture: "invalid/non-finite.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "alias",
			fixture: "invalid/alias.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "missing-required",
			fixture: "invalid/missing-required.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "duplicate-key",
			fixture: "invalid/duplicate-key.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "wrong-schema-version",
			fixture: "invalid/wrong-schema-version.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
		{
			name:    "wrong-document-type",
			fixture: "invalid/wrong-document-type.yaml",
			schema:  "metrics.schema.json",
			decode:  validateMetricsBytes,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := readFixture(t, test.fixture)
			if err := test.decode(data); err == nil {
				t.Fatalf("Go validator accepted an invalid fixture")
			}
			jsonData, jsonErr := yamlToJSON(t, data)
			if jsonErr == nil {
				if schemaErr := schemacheck.Validate(loadSchema(t, test.schema), jsonData); schemaErr == nil {
					t.Fatalf("JSON Schema accepted an invalid fixture")
				}
			}
		})
	}
}

func validateMetricsBytes(data []byte) error {
	document, err := DecodeMetrics(data)
	if err != nil {
		return err
	}
	if violations := document.Validate(); len(violations) > 0 {
		return violations[0]
	}
	return nil
}

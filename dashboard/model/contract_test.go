package model

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestGoValidatorMatchesSchema: every valid and invalid fixture yields
// the same pass/fail conclusion from the Go decoder/validator and the
// machine JSON Schema.
func TestGoValidatorMatchesSchema(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "schemas", "dashboard", "v1", "grafana-dashboard.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}

	validDir := filepath.Join("..", "..", "testdata", "dashboard", "model", "valid")
	for _, name := range []string{"minimal.json", "full.json"} {
		data, err := os.ReadFile(filepath.Join(validDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		dashboard, decodeErr := Decode(data)
		if decodeErr != nil || len(Validate(dashboard)) > 0 {
			t.Errorf("Go validator rejected valid fixture %s", name)
		}
		if err := schemacheck.Validate(schema, data); err != nil {
			t.Errorf("JSON Schema rejected valid fixture %s: %v", name, err)
		}
	}

	invalidDir := filepath.Join("..", "..", "testdata", "dashboard", "model", "invalid")
	entries, err := os.ReadDir(invalidDir)
	if err != nil {
		t.Fatalf("read invalid dir: %v", err)
	}
	// duplicate-key and duplicate-panel-id are Go-only rules: JSON Schema
	// cannot detect duplicate keys or cross-object ID uniqueness, so they
	// are compared for Go rejection only.
	schemaComparable := func(name string) bool {
		return name != "duplicate-key.json" && name != "duplicate-panel-id.json" && name != "missing-datasource-variable.json" && name != "bad-grid.json" && name != "bad-variable.json" && name != "external-link.json"
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(invalidDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		goRejected := false
		if dashboard, decodeErr := Decode(data); decodeErr != nil || len(Validate(dashboard)) > 0 {
			goRejected = true
		}
		if !goRejected {
			t.Errorf("fixture %s accepted by the Go validator", entry.Name())
			continue
		}
		if !schemaComparable(entry.Name()) {
			continue
		}
		schemaRejected := schemacheck.Validate(schema, data) != nil
		if !schemaRejected {
			t.Errorf("fixture %s rejected by Go but accepted by the JSON Schema", entry.Name())
		}
	}
}

// TestDocumentationJSONFences: the contract documentation's JSON fences
// must strictly decode and validate, so the documented contract cannot
// drift from the model.
func TestDocumentationJSONFences(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "docs", "14-dashboard-json-contract.md"))
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	fences := jsonFences(string(content))
	if len(fences) == 0 {
		t.Fatal("no JSON fences found in the contract documentation")
	}
	for index, fence := range fences {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(fence), &node); err != nil {
			t.Errorf("JSON fence %d does not parse: %v", index+1, err)
		}
		dashboard, decodeErr := Decode([]byte(fence))
		if decodeErr != nil {
			t.Errorf("JSON fence %d does not strictly decode: %v", index+1, decodeErr)
			continue
		}
		if violations := Validate(dashboard); len(violations) > 0 {
			t.Errorf("JSON fence %d fails semantic validation: %v", index+1, violations)
		}
	}
}

func jsonFences(content string) []string {
	var fences []string
	for {
		index := indexOf(content, "```json\n")
		if index < 0 {
			return fences
		}
		content = content[index+len("```json\n"):]
		end := indexOf(content, "```")
		if end < 0 {
			return fences
		}
		fences = append(fences, content[:end])
		content = content[end+3:]
	}
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}


package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestDocumentationYAMLSyntax parses every ```yaml fenced block in
// docs/jira/phase-1-jira-stories.md. This keeps documentation examples from
// drifting out of valid YAML.
func TestDocumentationYAMLSyntax(t *testing.T) {
	content := readDocumentation(t)
	blocks := yamlFences(content)
	if len(blocks) < 4 {
		t.Fatalf("expected at least 4 YAML fences in the documentation, found %d", len(blocks))
	}
	for index, block := range blocks {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(block), &node); err != nil {
			t.Errorf("documentation YAML block %d does not parse: %v", index+1, err)
		}
	}
}

// TestDocumentationSection8Examples are the three output contract examples
// in section 8. They must pass strict decoding, their v1 JSON Schema and the
// semantic validators.
func TestDocumentationSection8Examples(t *testing.T) {
	content := readDocumentation(t)
	examples := map[string]struct {
		heading string
		schema  string
		decode  func([]byte) (interface{ Validate() []*ValidationError }, error)
	}{
		"metrics": {
			heading: "### 8.1 `metrics.yaml`",
			schema:  "metrics.schema.json",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeMetrics(data)
			},
		},
		"otel": {
			heading: "### 8.2 `otel.yaml`",
			schema:  "otel.schema.json",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeOTel(data)
			},
		},
		"logging": {
			heading: "### 8.3 `logging.yaml`",
			schema:  "logging.schema.json",
			decode: func(data []byte) (interface{ Validate() []*ValidationError }, error) {
				return DecodeLogging(data)
			},
		},
	}

	for name, example := range examples {
		t.Run(name, func(t *testing.T) {
			block := yamlBlockAfter(content, example.heading)
			if block == "" {
				t.Fatalf("could not find YAML block after heading %q", example.heading)
			}
			data := []byte(block)
			document, err := example.decode(data)
			if err != nil {
				t.Fatalf("section 8 example does not decode strictly: %v", err)
			}
			if violations := document.Validate(); len(violations) > 0 {
				t.Fatalf("section 8 example fails semantic validation: %v", violations)
			}
			jsonData, err := yamlToJSON(t, data)
			if err != nil {
				t.Fatalf("section 8 example does not convert to JSON: %v", err)
			}
			if err := schemacheck.Validate(loadSchema(t, example.schema), jsonData); err != nil {
				t.Fatalf("section 8 example fails its v1 JSON Schema: %v", err)
			}
		})
	}
}

func readDocumentation(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "docs", "jira", "phase-1-jira-stories.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	return string(data)
}

var yamlFencePattern = regexp.MustCompile("(?s)```yaml\n(.*?)```")

func yamlFences(content string) []string {
	matches := yamlFencePattern.FindAllStringSubmatch(content, -1)
	blocks := make([]string, 0, len(matches))
	for _, match := range matches {
		blocks = append(blocks, match[1])
	}
	return blocks
}

func yamlBlockAfter(content, heading string) string {
	index := strings.Index(content, heading)
	if index < 0 {
		return ""
	}
	rest := content[index:]
	match := yamlFencePattern.FindStringSubmatch(rest)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

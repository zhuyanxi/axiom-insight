package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestMetricsFileContractDocumentation: the example in
// docs/07-metrics-file-contract.md must strictly decode, pass the
// semantic validator and the machine schema, so the documented contract
// cannot drift from the renderer output format.
func TestMetricsFileContractDocumentation(t *testing.T) {
	path := filepath.Join("..", "docs", "07-metrics-file-contract.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	block := firstYAMLBlock(t, string(data))
	document, err := DecodeMetrics([]byte(block))
	if err != nil {
		t.Fatalf("documented example does not decode strictly: %v\n%s", err, block)
	}
	if violations := document.Validate(); len(violations) > 0 {
		t.Fatalf("documented example fails semantic validation: %v", violations)
	}
	jsonData, err := yamlToJSON(t, []byte(block))
	if err != nil {
		t.Fatalf("documented example does not convert to JSON: %v", err)
	}
	if err := schemacheck.Validate(loadSchema(t, "metrics.schema.json"), jsonData); err != nil {
		t.Fatalf("documented example fails the machine schema: %v", err)
	}
	if document.SchemaVersion != SchemaVersionMetrics {
		t.Errorf("documented schema version = %q", document.SchemaVersion)
	}
}

// TestMetricsFileContractDocumentationCoversMapping: the binding mapping
// table must name every renderer-supported source.
func TestMetricsFileContractDocumentationCoversMapping(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "07-metrics-file-contract.md"))
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	content := string(data)
	for _, fragment := range []string{
		"PLAN_CONSTANT", "IR_CONSTANT", "RUNTIME_RESULT",
		"RUNTIME_RESOURCE", "RUNTIME_CONTEXT", "allowed_values",
		"GEN_RENDER_ERROR", "CheckPlanPolicyConsistency",
	} {
		if !strings.Contains(content, fragment) {
			t.Errorf("documentation lacks required fragment %q", fragment)
		}
	}
}

func firstYAMLBlock(t *testing.T, content string) string {
	t.Helper()
	pattern := regexp.MustCompile("(?s)```yaml\n(.*?)```")
	match := pattern.FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatal("no yaml fenced block found in the documentation")
	}
	return match[1]
}

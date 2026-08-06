package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestLoggingFileContractDocumentation: the example in
// docs/10-logging-file-contract.md must strictly decode, pass the
// semantic validator and the machine schema.
func TestLoggingFileContractDocumentation(t *testing.T) {
	path := filepath.Join("..", "docs", "10-logging-file-contract.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	block := firstYAMLBlock(t, string(data))
	document, err := DecodeLogging([]byte(block))
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
	if err := schemacheck.Validate(loadSchema(t, "logging.schema.json"), jsonData); err != nil {
		t.Fatalf("documented example fails the machine schema: %v", err)
	}
	if document.DocumentType != DocumentTypeLogging {
		t.Errorf("documented document type = %q", document.DocumentType)
	}
	if !document.Redaction.Immutable {
		t.Error("documented redaction must be immutable")
	}
}

// TestLoggingFileContractDocumentationCoversContract: the documentation
// must state the immutable redaction, condition order and optional-field
// semantics.
func TestLoggingFileContractDocumentationCoversContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "10-logging-file-contract.md"))
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	content := string(data)
	required := []string{
		"immutable: true",
		"runtime_clock",
		"runtime_context",
		"GEN_RENDER_ERROR",
		"省略字段",
		"status_in",
		"互斥",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Errorf("documentation lacks required fragment %q", fragment)
		}
	}
}

package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

// TestOTelFileContractDocumentation: the example in
// docs/09-otel-file-contract.md must strictly decode, pass the semantic
// validator and the machine schema, so the documented contract cannot
// drift from the renderer output format.
func TestOTelFileContractDocumentation(t *testing.T) {
	path := filepath.Join("..", "docs", "09-otel-file-contract.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	block := firstYAMLBlock(t, string(data))
	document, err := DecodeOTel([]byte(block))
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
	if err := schemacheck.Validate(loadSchema(t, "otel.schema.json"), jsonData); err != nil {
		t.Fatalf("documented example fails the machine schema: %v", err)
	}
	if document.PlanKind != OTelPlanKind {
		t.Errorf("documented plan kind = %q", document.PlanKind)
	}
	if document.SemanticConventionsVersion != OTelSemanticConventionsVersion {
		t.Errorf("documented semantic conventions version = %q", document.SemanticConventionsVersion)
	}
}

// TestOTelFileContractDocumentationCoversContract: the documentation must
// state the instrumentation-plan identity and every renderer rule.
func TestOTelFileContractDocumentationCoversContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "09-otel-file-contract.md"))
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	content := string(data)
	required := []string{
		"plan_kind: instrumentation",
		"instrumentation.tracing",
		"1.37.0",
		"extract_or_root",
		"current_context",
		"http_headers",
		"GEN_RENDER_ERROR",
		"OpenTelemetry Collector 配置",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Errorf("documentation lacks required fragment %q", fragment)
		}
	}
	for _, forbidden := range []string{"receivers:", "exporters:", "service.pipelines:"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("documentation must never present Collector fields (%q)", forbidden)
		}
	}
}

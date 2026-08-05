package tracing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRootSpanDocumentationCoversContract: the Root Span documentation
// must pin the semantic conventions version and every required fragment.
func TestRootSpanDocumentationCoversContract(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "08-tracing-root-spans.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read documentation: %v", err)
	}
	content := string(data)
	required := []string{
		"1.37.0",
		"extract_or_root",
		"new_root",
		"http_headers",
		"grpc_metadata",
		"service.name",
		"service.version",
		"code.namespace",
		"code.function",
		"http.request.method",
		"http.route",
		"rpc.system",
		"rpc.service",
		"rpc.method",
		"cron.job.name",
		"cron.job.schedule",
		"GEN_INCOMPLETE_TARGET",
		"GEN_UNSUPPORTED_ENTITY",
	}
	for _, fragment := range required {
		if !strings.Contains(content, fragment) {
			t.Errorf("documentation lacks required fragment %q", fragment)
		}
	}
	// The documentation must state that the version is fixed and never
	// resolves at runtime.
	if !strings.Contains(content, "固定") {
		t.Errorf("documentation must state the version is pinned")
	}
}

// TestSemanticConventionsVersionPinned: the pinned constant is fixed and
// matches the documentation.
func TestSemanticConventionsVersionPinned(t *testing.T) {
	if OTelSemanticConventionsVersion != "1.37.0" {
		t.Fatalf("semantic conventions version = %q, want 1.37.0", OTelSemanticConventionsVersion)
	}
}

package semantic

import (
	"os"
	"path/filepath"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/proto"
)

func TestToIREmptyDocument(t *testing.T) {
	got, err := ToIR(Document{})
	if err != nil {
		t.Fatalf("convert empty document: %v", err)
	}
	if got.SchemaVersion != defaultSchemaVersion {
		t.Fatalf("schema version = %q, want %q", got.SchemaVersion, defaultSchemaVersion)
	}
	if got.Service == nil {
		t.Fatal("service is nil")
	}
	if len(got.Packages) != 0 || len(got.Functions) != 0 || len(got.Dependencies) != 0 {
		t.Fatalf("empty document produced entities: %v", got)
	}
}

func TestToIRGeneratesDeterministicIDsAndOrdering(t *testing.T) {
	function := Function{
		Name:           "CreateOrder",
		QualifiedName:  "orders.CreateOrder",
		PackagePath:    "example.com/orders",
		SourceLocation: SourceLocation{RelativePath: "handler.go", StartLine: 10, StartColumn: 1},
	}
	functionID := FunctionID(function)
	document := Document{
		Service: Service{
			Name:       "orders",
			SourceRoot: "/workspace/orders",
			Language:   "go",
		},
		Packages: []Package{
			{Name: "z", ImportPath: "example.com/orders/z"},
			{Name: "a", ImportPath: "example.com/orders/a"},
		},
		Functions: []Function{function},
		Endpoints: []Endpoint{{
			Kind:           EndpointKindHTTPHandler,
			Name:           "CreateOrder",
			FunctionID:     functionID,
			SourceLocation: function.SourceLocation,
			HTTPMethod:     "POST",
			HTTPPath:       "/orders",
		}},
		Dependencies: []Dependency{{
			Kind:           DependencyKindRedis,
			Name:           "redis.Client.Get",
			FunctionID:     functionID,
			SourceLocation: SourceLocation{RelativePath: "handler.go", StartLine: 11, StartColumn: 2},
		}},
	}

	first, err := ToIR(document)
	if err != nil {
		t.Fatalf("first conversion: %v", err)
	}
	second, err := ToIR(document)
	if err != nil {
		t.Fatalf("second conversion: %v", err)
	}

	firstBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first IR: %v", err)
	}
	secondBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second IR: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("same semantic document produced different IR bytes")
	}

	if first.Functions[0].Id != functionID {
		t.Fatalf("function ID = %q, want %q", first.Functions[0].Id, functionID)
	}
	if first.Packages[0].ImportPath != "example.com/orders/a" {
		t.Fatalf("packages not sorted by ID: %v", first.Packages)
	}
	if first.Endpoints[0].SourceLocation.RelativePath != "handler.go" {
		t.Fatalf("location path = %q, want relative path", first.Endpoints[0].SourceLocation.RelativePath)
	}
	if first.Dependencies[0].Kind != observabilityv1.DependencyKind_REDIS {
		t.Fatalf("dependency kind = %v, want REDIS", first.Dependencies[0].Kind)
	}
}

func TestToIRDiagnostics(t *testing.T) {
	base := t.TempDir()
	outsideRoot := filepath.Join(base, "outside", "run.go")
	if err := os.MkdirAll(filepath.Dir(outsideRoot), 0o755); err != nil {
		t.Fatalf("create outside-root directory: %v", err)
	}
	if err := os.WriteFile(outsideRoot, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("seed outside-root source file: %v", err)
	}
	sourceRoot := filepath.Join(base, "workspace", "orders")
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		t.Fatalf("create source root: %v", err)
	}

	tests := []struct {
		name string
		doc  Document
		want []string
	}{
		{
			name: "missing function location",
			doc:  Document{Functions: []Function{{Name: "Run"}}},
			want: []string{"MISSING_SOURCE_LOCATION"},
		},
		{
			name: "unknown dependency kind",
			doc:  Document{Dependencies: []Dependency{{Kind: DependencyKind("CUSTOM"), Name: "custom.Client"}}},
			want: []string{"UNKNOWN_DEPENDENCY_KIND", "MISSING_SOURCE_LOCATION"},
		},
		{
			name: "unresolved call",
			doc:  Document{CallEdges: []CallEdge{{CallerFunctionID: "fn:caller", Resolution: CallResolutionUnresolved}}},
			want: []string{"UNRESOLVED_CALL", "MISSING_SOURCE_LOCATION"},
		},
		{
			name: "absolute path outside root",
			doc: Document{
				Service:   Service{SourceRoot: sourceRoot},
				Functions: []Function{{Name: "Run", SourceLocation: SourceLocation{RelativePath: outsideRoot, StartLine: 1}}},
			},
			want: []string{"INVALID_SOURCE_PATH"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ToIR(test.doc)
			if err != nil {
				t.Fatalf("convert document: %v", err)
			}
			codes := make(map[string]bool, len(got.Diagnostics))
			for _, diagnostic := range got.Diagnostics {
				codes[diagnostic.Code] = true
			}
			for _, code := range test.want {
				if !codes[code] {
					t.Fatalf("diagnostics missing %q: %v", code, got.Diagnostics)
				}
			}
		})
	}
}

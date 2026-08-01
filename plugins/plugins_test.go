package plugins

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGoAnalyzerMetadata(t *testing.T) {
	metadata, err := NewGoAnalyzer().GetMetadata(context.Background(), &observabilityv1.GetMetadataRequest{})
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	if metadata.Language != LanguageGo || metadata.PluginVersion != PluginVersion {
		t.Fatalf("metadata identity = %+v", metadata)
	}
	if metadata.MinSchemaVersion != MinSchemaVersion || metadata.MaxSchemaVersion != MaxSchemaVersion {
		t.Fatalf("metadata schema range = %+v", metadata)
	}
	wantCapabilities := []string{
		"http_handlers",
		"grpc_handlers",
		"cron_jobs",
		"kafka_consumers",
		"kafka_producers",
		"sql",
		"redis",
		"http_clients",
		"rpc_clients",
	}
	if !reflect.DeepEqual(metadata.Capabilities, wantCapabilities) {
		t.Fatalf("metadata capabilities = %v, want %v", metadata.Capabilities, wantCapabilities)
	}
}

func TestGoAnalyzerAnalyzeReturnsIRAndDiagnostics(t *testing.T) {
	root := writeProject(t, map[string]string{
		"go.mod":              "module example.com/pluginfixture\n\ngo 1.26.1\n",
		"run.go":              "package pluginfixture\n\nfunc Run() {}\n",
		"internal/ignored.go": "package ignored\n\nfunc Ignored() {}\n",
	})

	response, err := NewGoAnalyzer().Analyze(context.Background(), &observabilityv1.AnalyzeRequest{
		SourceRoot:    root,
		Include:       []string{"./..."},
		Exclude:       []string{"internal/..."},
		Config:        "service:\n  name: plugin-service\n",
		SchemaVersion: CurrentSchemaVersion,
	})
	if err != nil {
		t.Fatalf("analyze fixture: %v", err)
	}
	if response.Document == nil {
		t.Fatal("analyze response has no document")
	}
	if response.Document.Service.Name != "plugin-service" {
		t.Fatalf("service name = %q, want plugin-service", response.Document.Service.Name)
	}
	if len(response.Document.Functions) != 1 || response.Document.Functions[0].Name != "Run" {
		t.Fatalf("functions = %+v, want only Run", response.Document.Functions)
	}
	if len(response.Diagnostics) != len(response.Document.Diagnostics) {
		t.Fatalf("response diagnostics = %d, document diagnostics = %d", len(response.Diagnostics), len(response.Document.Diagnostics))
	}
}

func TestGoAnalyzerRejectsInvalidSourceRoot(t *testing.T) {
	_, err := NewGoAnalyzer().Analyze(context.Background(), &observabilityv1.AnalyzeRequest{
		SourceRoot: filepath.Join(t.TempDir(), "missing"),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error code = %s, want %s: %v", status.Code(err), codes.InvalidArgument, err)
	}
	if !strings.Contains(err.Error(), "source root") {
		t.Fatalf("error = %q, want source root detail", err)
	}
}

func TestGoAnalyzerRejectsIncompatibleSchemaVersionBeforeAnalysis(t *testing.T) {
	_, err := NewGoAnalyzer().Analyze(context.Background(), &observabilityv1.AnalyzeRequest{
		SourceRoot:    filepath.Join(t.TempDir(), "missing"),
		SchemaVersion: "v2",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error code = %s, want %s: %v", status.Code(err), codes.FailedPrecondition, err)
	}
	if !strings.Contains(err.Error(), "schema version") || !strings.Contains(err.Error(), "v1..v1") {
		t.Fatalf("error = %q, want schema range detail", err)
	}
}

func TestInProcessTransportReturnsDefaultAnalyzer(t *testing.T) {
	transport := NewInProcessTransport(nil)
	analyzer, err := transport.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect in-process transport: %v", err)
	}
	if _, ok := analyzer.(*GoAnalyzer); !ok {
		t.Fatalf("analyzer type = %T, want *GoAnalyzer", analyzer)
	}
	if err := transport.Close(); err != nil {
		t.Fatalf("close in-process transport: %v", err)
	}
}

func TestGRPCServerDelegatesAnalyzer(t *testing.T) {
	server := NewGRPCServer(nil)
	metadata, err := server.GetMetadata(context.Background(), &observabilityv1.GetMetadataRequest{})
	if err != nil {
		t.Fatalf("get metadata through gRPC server: %v", err)
	}
	if metadata.Language != LanguageGo {
		t.Fatalf("metadata language = %q, want %q", metadata.Language, LanguageGo)
	}
}

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture file: %v", err)
		}
	}
	return root
}

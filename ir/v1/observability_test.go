package observabilityv1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestObservabilityDocumentRoundTrip(t *testing.T) {
	want := &ObservabilityDocument{
		SchemaVersion: "v1",
		Service: &Service{
			Name:       "orders",
			SourceRoot: "./",
			Language:   "go",
			PackageIds: []string{"pkg:orders"},
		},
		Packages: []*Package{{
			Id:         "pkg:orders",
			Name:       "orders",
			ImportPath: "example.com/orders",
			Files:      []string{"handler.go"},
		}},
		Functions: []*Function{{
			Id:            "fn:orders.CreateOrder",
			Name:          "CreateOrder",
			QualifiedName: "orders.CreateOrder",
			PackageId:     "pkg:orders",
			PackagePath:   "example.com/orders",
			Signature:     "func() error",
			SourceLocation: &SourceLocation{
				RelativePath: "handler.go",
				StartLine:    10,
				StartColumn:  1,
				EndLine:      12,
				EndColumn:    2,
			},
			Exported:          true,
			InputEndpointIds:  []string{"endpoint:http:create-order"},
			DependencyIds:     []string{"dependency:redis"},
			CalleeFunctionIds: []string{"fn:orders.publish"},
		}},
		Endpoints: []*Endpoint{{
			Id:         "endpoint:http:create-order",
			Kind:       EndpointKind_HTTP_HANDLER,
			Name:       "CreateOrder",
			FunctionId: "fn:orders.CreateOrder",
			HttpMethod: "POST",
			HttpPath:   "/orders",
		}},
		Dependencies: []*Dependency{{
			Id:            "dependency:redis",
			Kind:          DependencyKind_REDIS,
			Name:          "redis.Client.Get",
			FunctionId:    "fn:orders.CreateOrder",
			Operation:     "get",
			TargetPackage: "github.com/redis/go-redis/v9",
			Value:         "order:{id}",
			ValueIsStatic: true,
		}},
		CallEdges: []*CallEdge{{
			Id:               "edge:create-order-publish",
			CallerFunctionId: "fn:orders.CreateOrder",
			CalleeFunctionId: "fn:orders.publish",
			Resolution:       CallResolution_RESOLVED,
		}},
		Diagnostics: []*Diagnostic{{
			Severity: DiagnosticSeverity_WARNING,
			Code:     "UNRESOLVED_CALL",
			Message:  "interface call target could not be resolved",
		}},
	}

	binary, err := proto.MarshalOptions{Deterministic: true}.Marshal(want)
	if err != nil {
		t.Fatalf("marshal IR: %v", err)
	}

	got := new(ObservabilityDocument)
	if err := proto.Unmarshal(binary, got); err != nil {
		t.Fatalf("unmarshal IR: %v", err)
	}
	if !proto.Equal(want, got) {
		t.Fatalf("round-trip mismatch:\nwant: %v\ngot:  %v", want, got)
	}
}

func TestObservabilityDocumentJSONUsesStableReadableNames(t *testing.T) {
	document := &ObservabilityDocument{
		SchemaVersion: "v1",
		Endpoints:     []*Endpoint{{Kind: EndpointKind_HTTP_HANDLER}},
	}

	json, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(document)
	if err != nil {
		t.Fatalf("marshal IR JSON: %v", err)
	}

	text := string(json)
	for _, field := range []string{"schema_version", "endpoints", "kind"} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON missing stable field %q: %s", field, text)
		}
	}
	if !strings.Contains(text, "HTTP_HANDLER") {
		t.Fatalf("JSON missing enum name: %s", text)
	}
}

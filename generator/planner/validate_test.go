package planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// TestValidateDocumentRejectsInvalidIR covers every structural failure:
// nil document, missing service, empty service name, unsupported schema,
// empty and duplicate IDs.
func TestValidateDocumentRejectsInvalidIR(t *testing.T) {
	tests := []struct {
		name      string
		document  func() *observabilityv1.ObservabilityDocument
		wantCode  string
		wantField string
	}{
		{
			name:      "nil document",
			document:  func() *observabilityv1.ObservabilityDocument { return nil },
			wantCode:  "GEN_INVALID_IR",
			wantField: "$",
		},
		{
			name:      "missing service",
			document:  func() *observabilityv1.ObservabilityDocument { document := testDocument(); document.Service = nil; return document },
			wantCode:  "GEN_INVALID_IR",
			wantField: "service",
		},
		{
			name:      "empty service name",
			document:  func() *observabilityv1.ObservabilityDocument { document := testDocument(); document.Service.Name = ""; return document },
			wantCode:  "GEN_INVALID_IR",
			wantField: "service.name",
		},
		{
			name:      "unsupported schema version",
			document:  func() *observabilityv1.ObservabilityDocument { document := testDocument(); document.SchemaVersion = "v99"; return document },
			wantCode:  "GEN_UNSUPPORTED_SCHEMA",
			wantField: "schema_version",
		},
		{
			name:      "empty schema version",
			document:  func() *observabilityv1.ObservabilityDocument { document := testDocument(); document.SchemaVersion = ""; return document },
			wantCode:  "GEN_UNSUPPORTED_SCHEMA",
			wantField: "schema_version",
		},
		{
			name: "duplicate function ID",
			document: func() *observabilityv1.ObservabilityDocument {
				document := testDocument()
				document.Functions = append(document.Functions, &observabilityv1.Function{Id: "fn:handler"})
				return document
			},
			wantCode:  "GEN_INVALID_IR",
			wantField: "fn:handler.id",
		},
		{
			name: "empty endpoint ID",
			document: func() *observabilityv1.ObservabilityDocument {
				document := testDocument()
				document.Endpoints = append(document.Endpoints, &observabilityv1.Endpoint{})
				return document
			},
			wantCode:  "GEN_INVALID_IR",
			wantField: "endpoint.id",
		},
		{
			name: "duplicate dependency ID",
			document: func() *observabilityv1.ObservabilityDocument {
				document := testDocument()
				document.Dependencies = append(document.Dependencies, &observabilityv1.Dependency{Id: "dep:orders-sql"})
				return document
			},
			wantCode:  "GEN_INVALID_IR",
			wantField: "dep:orders-sql.id",
		},
		{
			name: "empty call edge ID",
			document: func() *observabilityv1.ObservabilityDocument {
				document := testDocument()
				document.CallEdges = append(document.CallEdges, &observabilityv1.CallEdge{})
				return document
			},
			wantCode:  "GEN_INVALID_IR",
			wantField: "call_edge.id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, violations, err := validateDocument(context.Background(), test.document())
			if err != nil {
				t.Fatalf("validateDocument failed unexpectedly: %v", err)
			}
			if len(violations) == 0 {
				t.Fatalf("expected violations for %s", test.name)
			}
			found := false
			for _, violation := range violations {
				if violation.Code == test.wantCode && violationLocation(violation) == test.wantField {
					found = true
				}
			}
			if !found {
				t.Errorf("no violation with code %s at %s; got %v", test.wantCode, test.wantField, violations)
			}
		})
	}
}

// TestValidateDocumentDanglingReferences covers every reference type.
func TestValidateDocumentDanglingReferences(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*observabilityv1.ObservabilityDocument)
		wantCode  string
		wantField string
	}{
		{
			name:      "endpoint function",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Endpoints[0].FunctionId = "fn:missing" },
			wantField: "function_id",
		},
		{
			name:      "dependency function",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Dependencies[0].FunctionId = "fn:missing" },
			wantField: "function_id",
		},
		{
			name:      "function input endpoint",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Functions[0].InputEndpointIds = []string{"ep:missing"} },
			wantField: "input_endpoint_ids",
		},
		{
			name:      "function output endpoint",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Functions[0].OutputEndpointIds = []string{"ep:missing"} },
			wantField: "output_endpoint_ids",
		},
		{
			name:      "function dependency",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Functions[0].DependencyIds = []string{"dep:missing"} },
			wantField: "dependency_ids",
		},
		{
			name:      "function caller",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Functions[0].CallerFunctionIds = []string{"fn:missing"} },
			wantField: "caller_function_ids",
		},
		{
			name:      "function callee",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.Functions[0].CalleeFunctionIds = []string{"fn:missing"} },
			wantField: "callee_function_ids",
		},
		{
			name:      "call edge caller",
			mutate:    func(document *observabilityv1.ObservabilityDocument) { document.CallEdges[0].CallerFunctionId = "fn:missing" },
			wantField: "caller_function_id",
		},
		{
			name: "resolved edge missing callee",
			mutate: func(document *observabilityv1.ObservabilityDocument) {
				document.CallEdges[0].CalleeFunctionId = "fn:missing"
				document.CallEdges[0].Resolution = observabilityv1.CallResolution_RESOLVED
			},
			wantField: "callee_function_id",
		},
		{
			name: "unresolved edge with missing callee",
			mutate: func(document *observabilityv1.ObservabilityDocument) {
				document.CallEdges[0].CalleeFunctionId = "fn:missing"
				document.CallEdges[0].Resolution = observabilityv1.CallResolution_UNRESOLVED
			},
			wantField: "callee_function_id",
		},
		{
			name: "unspecified resolution",
			mutate: func(document *observabilityv1.ObservabilityDocument) {
				document.CallEdges[0].Resolution = observabilityv1.CallResolution_CALL_RESOLUTION_UNSPECIFIED
			},
			wantCode:  "GEN_INVALID_IR",
			wantField: "resolution",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := testDocument()
			test.mutate(document)
			_, violations, err := validateDocument(context.Background(), document)
			if err != nil {
				t.Fatalf("validateDocument failed unexpectedly: %v", err)
			}
			wantCode := test.wantCode
			if wantCode == "" {
				wantCode = "GEN_DANGLING_REFERENCE"
			}
			found := false
			for _, violation := range violations {
				if violation.Code == wantCode && violation.Field == test.wantField {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s at %s; got %v", wantCode, test.wantField, violations)
			}
		})
	}
}

// TestValidateDocumentAllowsUnresolvedEdgeWithoutCallee: an unresolved
// edge with an empty callee is valid.
func TestValidateDocumentAllowsUnresolvedEdgeWithoutCallee(t *testing.T) {
	document := testDocument()
	document.CallEdges[0].CalleeFunctionId = ""
	document.CallEdges[0].Resolution = observabilityv1.CallResolution_UNRESOLVED
	_, violations, err := validateDocument(context.Background(), document)
	if err != nil {
		t.Fatalf("validateDocument failed unexpectedly: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("unexpected violations: %v", violations)
	}
}

// TestPlanNilDocumentFails: Plan(nil) reports GEN_INVALID_IR without
// panicking.
func TestPlanNilDocumentFails(t *testing.T) {
	planner, _, _, _ := newTestPlanner()
	plan, _, err := planner.Plan(context.Background(), nil, defaultPolicy())
	if plan != nil {
		t.Fatal("plan must be nil")
	}
	if err == nil || !strings.Contains(err.Error(), "GEN_INVALID_IR") {
		t.Fatalf("error = %v, want GEN_INVALID_IR", err)
	}
	var invalid *InvalidIRError
	if !errors.As(err, &invalid) {
		t.Fatalf("error type = %T", err)
	}
}

func violationLocation(violation ValidationError) string {
	if violation.EntityID == "" {
		return violation.Field
	}
	return violation.EntityID + "." + violation.Field
}

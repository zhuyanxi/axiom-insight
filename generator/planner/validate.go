package planner

import (
	"context"
	"fmt"
	"strings"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// ValidationError describes one IR violation found before planning. The
// code is GEN_INVALID_IR for structural problems and
// GEN_DANGLING_REFERENCE for missing or mismatched references.
type ValidationError struct {
	// Code is the stable message code.
	Code string
	// EntityID is the offending IR entity ID (empty for document-level
	// violations).
	EntityID string
	// Field locates the offending field inside the entity.
	Field string
	// Message explains the rule; it never contains source content.
	Message string
}

// Error implements error.
func (violation *ValidationError) Error() string {
	location := violation.Field
	if violation.EntityID != "" {
		location = violation.EntityID + "." + violation.Field
	}
	return fmt.Sprintf("%s: %s: %s", violation.Code, location, violation.Message)
}

// InvalidIRError aggregates every structural violation; a plan is never
// produced when this error is returned.
type InvalidIRError struct {
	violations []ValidationError
}

// Error implements error, one line per violation.
func (failure *InvalidIRError) Error() string {
	lines := make([]string, 0, len(failure.violations))
	for _, violation := range failure.violations {
		lines = append(lines, violation.Error())
	}
	return strings.Join(lines, "\n")
}

// Violations returns the individual violations. The returned slice must
// not be modified.
func (failure *InvalidIRError) Violations() []ValidationError { return failure.violations }

// validateDocument checks document presence, service identity, schema
// support, entity IDs and every cross-entity reference. It runs in one
// pass, checks context cancellation periodically and never panics. The
// document is never modified.
func validateDocument(ctx context.Context, document *observabilityv1.ObservabilityDocument) (*Index, []ValidationError, error) {
	if document == nil {
		return nil, []ValidationError{{
			Code: "GEN_INVALID_IR", Field: "$",
			Message: "document is nil",
		}}, nil
	}
	if document.GetService() == nil {
		return nil, []ValidationError{{
			Code: "GEN_INVALID_IR", Field: "service",
			Message: "document has no service",
		}}, nil
	}
	if document.GetService().GetName() == "" {
		return nil, []ValidationError{{
			Code: "GEN_INVALID_IR", EntityID: "service", Field: "name",
			Message: "service name is empty",
		}}, nil
	}
	if document.GetSchemaVersion() != SupportedIRSchemaVersion {
		return nil, []ValidationError{{
			Code: "GEN_UNSUPPORTED_SCHEMA", Field: "schema_version",
			Message: "unsupported IR schema version " + fmt.Sprintf("%q", document.GetSchemaVersion()) + "; supported: " + SupportedIRSchemaVersion,
		}}, nil
	}

	index := newIndex()
	var violations []ValidationError

	index.functions = make(map[string]*observabilityv1.Function, len(document.Functions))
	for _, function := range document.Functions {
		if err := contextError(ctx); err != nil {
			return nil, violations, err
		}
		if function.GetId() == "" {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: "function", Field: "id",
				Message: "function ID is empty",
			})
			continue
		}
		if index.functions[function.GetId()] != nil {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: function.GetId(), Field: "id",
				Message: "duplicate function ID",
			})
			continue
		}
		index.functions[function.GetId()] = function
	}

	index.endpoints = make(map[string]*observabilityv1.Endpoint, len(document.Endpoints))
	for _, endpoint := range document.Endpoints {
		if err := contextError(ctx); err != nil {
			return nil, violations, err
		}
		if endpoint.GetId() == "" {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: "endpoint", Field: "id",
				Message: "endpoint ID is empty",
			})
			continue
		}
		if index.endpoints[endpoint.GetId()] != nil {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: endpoint.GetId(), Field: "id",
				Message: "duplicate endpoint ID",
			})
			continue
		}
		index.endpoints[endpoint.GetId()] = endpoint
	}

	index.dependencies = make(map[string]*observabilityv1.Dependency, len(document.Dependencies))
	for _, dependency := range document.Dependencies {
		if err := contextError(ctx); err != nil {
			return nil, violations, err
		}
		if dependency.GetId() == "" {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: "dependency", Field: "id",
				Message: "dependency ID is empty",
			})
			continue
		}
		if index.dependencies[dependency.GetId()] != nil {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: dependency.GetId(), Field: "id",
				Message: "duplicate dependency ID",
			})
			continue
		}
		index.dependencies[dependency.GetId()] = dependency
	}

	index.callEdges = make(map[string]*observabilityv1.CallEdge, len(document.CallEdges))
	for _, edge := range document.CallEdges {
		if err := contextError(ctx); err != nil {
			return nil, violations, err
		}
		if edge.GetId() == "" {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: "call_edge", Field: "id",
				Message: "call edge ID is empty",
			})
			continue
		}
		if index.callEdges[edge.GetId()] != nil {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: edge.GetId(), Field: "id",
				Message: "duplicate call edge ID",
			})
			continue
		}
		index.callEdges[edge.GetId()] = edge
	}

	violations = append(violations, validateReferences(document, index)...)
	return index, violations, nil
}

// validateReferences checks every cross-entity reference. Iteration
// order is fixed (functions, endpoints, dependencies, call edges) so the
// violation list is deterministic.
func validateReferences(document *observabilityv1.ObservabilityDocument, index *Index) []ValidationError {
	var violations []ValidationError

	for _, function := range document.Functions {
		if function.GetId() == "" {
			continue
		}
		for _, endpointID := range function.InputEndpointIds {
			if index.Endpoint(endpointID) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: function.GetId(), Field: "input_endpoint_ids",
					Message: "references missing endpoint " + endpointID,
				})
			}
		}
		for _, endpointID := range function.OutputEndpointIds {
			if index.Endpoint(endpointID) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: function.GetId(), Field: "output_endpoint_ids",
					Message: "references missing endpoint " + endpointID,
				})
			}
		}
		for _, dependencyID := range function.DependencyIds {
			if index.Dependency(dependencyID) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: function.GetId(), Field: "dependency_ids",
					Message: "references missing dependency " + dependencyID,
				})
			}
		}
		for _, callerID := range function.CallerFunctionIds {
			if index.Function(callerID) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: function.GetId(), Field: "caller_function_ids",
					Message: "references missing function " + callerID,
				})
			}
		}
		for _, calleeID := range function.CalleeFunctionIds {
			if index.Function(calleeID) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: function.GetId(), Field: "callee_function_ids",
					Message: "references missing function " + calleeID,
				})
			}
		}
	}

	for _, endpoint := range document.Endpoints {
		if endpoint.GetId() == "" {
			continue
		}
		if index.Function(endpoint.GetFunctionId()) == nil {
			violations = append(violations, ValidationError{
				Code: "GEN_DANGLING_REFERENCE", EntityID: endpoint.GetId(), Field: "function_id",
				Message: "references missing function " + endpoint.GetFunctionId(),
			})
		}
	}

	for _, dependency := range document.Dependencies {
		if dependency.GetId() == "" {
			continue
		}
		if index.Function(dependency.GetFunctionId()) == nil {
			violations = append(violations, ValidationError{
				Code: "GEN_DANGLING_REFERENCE", EntityID: dependency.GetId(), Field: "function_id",
				Message: "references missing function " + dependency.GetFunctionId(),
			})
		}
	}

	for _, edge := range document.CallEdges {
		if edge.GetId() == "" {
			continue
		}
		if index.Function(edge.GetCallerFunctionId()) == nil {
			violations = append(violations, ValidationError{
				Code: "GEN_DANGLING_REFERENCE", EntityID: edge.GetId(), Field: "caller_function_id",
				Message: "references missing function " + edge.GetCallerFunctionId(),
			})
		}
		// Resolution consistency: a RESOLVED edge must name an existing
		// callee; an UNRESOLVED edge may carry an empty callee or a
		// guessed callee that must still exist.
		switch edge.GetResolution() {
		case observabilityv1.CallResolution_RESOLVED:
			if index.Function(edge.GetCalleeFunctionId()) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: edge.GetId(), Field: "callee_function_id",
					Message: "resolved edge references missing function " + edge.GetCalleeFunctionId(),
				})
			}
		case observabilityv1.CallResolution_UNRESOLVED:
			if edge.GetCalleeFunctionId() != "" && index.Function(edge.GetCalleeFunctionId()) == nil {
				violations = append(violations, ValidationError{
					Code: "GEN_DANGLING_REFERENCE", EntityID: edge.GetId(), Field: "callee_function_id",
					Message: "unresolved edge references missing function " + edge.GetCalleeFunctionId(),
				})
			}
		default:
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: edge.GetId(), Field: "resolution",
				Message: "call edge resolution is unspecified",
			})
		}
	}

	return violations
}

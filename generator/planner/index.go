package planner

import observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"

// Index maps every IR entity ID to its entity, built in a single pass
// during validation. Lookups are read-only; sub-planners use it to
// resolve targets without re-scanning the document.
type Index struct {
	functions    map[string]*observabilityv1.Function
	endpoints    map[string]*observabilityv1.Endpoint
	dependencies map[string]*observabilityv1.Dependency
	callEdges    map[string]*observabilityv1.CallEdge
}

func newIndex() *Index {
	return &Index{}
}

// Function returns the function with the given ID, or nil.
func (index *Index) Function(id string) *observabilityv1.Function {
	return index.functions[id]
}

// Endpoint returns the endpoint with the given ID, or nil.
func (index *Index) Endpoint(id string) *observabilityv1.Endpoint {
	return index.endpoints[id]
}

// Dependency returns the dependency with the given ID, or nil.
func (index *Index) Dependency(id string) *observabilityv1.Dependency {
	return index.dependencies[id]
}

// CallEdge returns the call edge with the given ID, or nil.
func (index *Index) CallEdge(id string) *observabilityv1.CallEdge {
	return index.callEdges[id]
}

// Kind reports the IR entity kind of an ID and whether it exists.
func (index *Index) Kind(id string) (observabilityv1.TargetKind, bool) {
	if index.functions[id] != nil {
		return observabilityv1.TargetKind_TARGET_KIND_FUNCTION, true
	}
	if index.endpoints[id] != nil {
		return observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, true
	}
	if index.dependencies[id] != nil {
		return observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, true
	}
	if index.callEdges[id] != nil {
		return observabilityv1.TargetKind_TARGET_KIND_CALL_EDGE, true
	}
	return observabilityv1.TargetKind_TARGET_KIND_UNSPECIFIED, false
}

// Counts returns the number of entities per type, for reports.
func (index *Index) Counts() (functions, endpoints, dependencies, callEdges int) {
	return len(index.functions), len(index.endpoints), len(index.dependencies), len(index.callEdges)
}

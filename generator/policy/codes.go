package policy

// Generator diagnostic codes (Phase 1 contract, section 5.1). Codes are
// stable message identifiers; P1-03 uses CodeInvalidConfig and the
// remaining codes are reserved for later Phase 1 stories (Planner,
// Renderers, CLI Writer).
const (
	// CodeUnsupportedSchema: the input IR schema version is not supported.
	CodeUnsupportedSchema = "GEN_UNSUPPORTED_SCHEMA"
	// CodeInvalidIR: the IR misses the service, contains duplicate IDs or
	// is structurally invalid.
	CodeInvalidIR = "GEN_INVALID_IR"
	// CodeDanglingReference: a reference required by the plan does not exist.
	CodeDanglingReference = "GEN_DANGLING_REFERENCE"
	// CodeInvalidConfig: a configuration value, scope or combination is
	// invalid. Used by this package for every si.yaml `generation` error.
	CodeInvalidConfig = "GEN_INVALID_CONFIG"
	// CodeNameCollision: normalized names collide and were disambiguated.
	CodeNameCollision = "GEN_NAME_COLLISION"
	// CodeCardinalityBlocked: a high-cardinality attribute was removed.
	CodeCardinalityBlocked = "GEN_CARDINALITY_BLOCKED"
	// CodeCardinalityLimitExceeded: estimated instruments or series exceed
	// the policy limit.
	CodeCardinalityLimitExceeded = "GEN_CARDINALITY_LIMIT_EXCEEDED"
	// CodeSensitiveValueDropped: a potentially sensitive value was removed.
	CodeSensitiveValueDropped = "GEN_SENSITIVE_VALUE_DROPPED"
	// CodeUnsupportedEntity: an entity cannot be mapped safely.
	CodeUnsupportedEntity = "GEN_UNSUPPORTED_ENTITY"
	// CodeIncompleteTarget: a dynamic or unknown target caused partial
	// attribute loss.
	CodeIncompleteTarget = "GEN_INCOMPLETE_TARGET"
	// CodeRenderError: a plan cannot be rendered as a valid document.
	CodeRenderError = "GEN_RENDER_ERROR"
	// CodeOutputExists: a target file exists and --force was not given.
	CodeOutputExists = "GEN_OUTPUT_EXISTS"
	// CodeUnsafeTarget: a selected output target (or the output
	// directory) is a symlink or not a regular file.
	CodeUnsafeTarget = "GEN_UNSAFE_TARGET"
)

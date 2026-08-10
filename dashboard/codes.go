package dashboard

// Dashboard diagnostic codes (Phase 2 contract, section 2.7). Codes are
// stable message identifiers; P2-01 uses the structural and reference
// codes, later stories use the panel and render codes.
const (
	// CodeUnsupportedSchema: the IR, Plan or Dashboard schema version is
	// not supported.
	CodeUnsupportedSchema = "DASHBOARD_UNSUPPORTED_SCHEMA"
	// CodeInvalidIR: the input IR or Plan is structurally invalid.
	CodeInvalidIR = "DASHBOARD_INVALID_IR"
	// CodeInvalidConfig: the Dashboard configuration is invalid or over a
	// limit.
	CodeInvalidConfig = "DASHBOARD_INVALID_CONFIG"
	// CodeDanglingReference: a Query/Panel/Link references a missing Plan
	// item.
	CodeDanglingReference = "DASHBOARD_DANGLING_REFERENCE"
	// CodeMissingRequiredMetric: an optional panel's required metric or
	// label is not declared by the Plan.
	CodeMissingRequiredMetric = "DASHBOARD_MISSING_REQUIRED_METRIC"
	// CodeUnsupportedTarget: an entity has no safe v1 dashboard mapping.
	CodeUnsupportedTarget = "DASHBOARD_UNSUPPORTED_TARGET"
	// CodeEmptyCategory: a category has no generatable panel; the row is
	// omitted.
	CodeEmptyCategory = "DASHBOARD_EMPTY_CATEGORY"
	// CodePanelLimitExceeded: the estimated panel or query count exceeds
	// the policy limit.
	CodePanelLimitExceeded = "DASHBOARD_PANEL_LIMIT_EXCEEDED"
	// CodeNameCollision: a normalized UID, variable or title collides and
	// was disambiguated.
	CodeNameCollision = "DASHBOARD_NAME_COLLISION"
	// CodeSensitiveValueDropped: a potentially sensitive IR value was
	// blocked from entering the dashboard.
	CodeSensitiveValueDropped = "DASHBOARD_SENSITIVE_VALUE_DROPPED"
	// CodeRenderError: the typed model cannot be rendered or validated as
	// legal JSON.
	CodeRenderError = "DASHBOARD_RENDER_ERROR"
	// CodeOutputExists: the target exists and --force was not given.
	CodeOutputExists = "DASHBOARD_OUTPUT_EXISTS"
	// CodeUnsafeTarget: the output target is a symlink or not a regular
	// file.
	CodeUnsafeTarget = "DASHBOARD_UNSAFE_TARGET"
)

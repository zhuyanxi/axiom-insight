// Package logging implements the logging signal sub-planner for the
// Planner pipeline (Phase 1, P1-12): LoggingPlanner turns endpoints and
// dependencies into structured log event plans with correlation and
// redaction semantics.
//
// Rules: event families are exhaustively mapped per EndpointKind and
// DependencyKind (unknown kinds produce GEN_UNSUPPORTED_ENTITY and are
// skipped); completed and failed events are mutually exclusive by status;
// severity has one documented mapping per runtime status with no
// fallthrough; correlation fields are runtime context bindings only —
// no fake or generator-produced IDs; the built-in credential denylist is
// unclosable and every generated field and constant passes the unified
// policy before entering the plan; diagnostics never echo rejected
// values; field sets stay within fixed limits and are deterministically
// sorted.
//
// Ownership boundary: this package never imports the Go analyzer, AST
// packages, the plugin transport or the generated-document contracts.
package logging

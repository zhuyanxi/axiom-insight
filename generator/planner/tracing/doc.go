// Package tracing implements the tracing signal sub-planners for the
// Planner pipeline (Phase 1, P1-09/P1-10): EndpointRootSpanPlanner turns
// HTTP, gRPC and Cron endpoints into one Root Span plan each.
//
// Rules: the EndpointKind mapping is exhaustive (unknown kinds produce
// GEN_UNSUPPORTED_ENTITY and are skipped); span names follow the P1-04
// rules and never contain raw URLs, queries, payloads or source paths;
// attribute vocabulary is pinned to the OpenTelemetry Semantic
// Conventions 1.37.0 keys and the unified service/module/function/
// operation vocabulary (no duplicate aliases); parent strategies and
// carriers are abstract identifiers only — no runtime carrier or request
// is ever read; a plan is a binding description, never an SDK call.
//
// Ownership boundary: this package never imports the Go analyzer, AST
// packages, the plugin transport or the generated-document contracts; it
// consumes only the validated IR, the generation Policy, the
// naming/attribute policy and the Planner contracts.
package tracing

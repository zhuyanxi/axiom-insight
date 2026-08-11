// Package dashboard owns the Dashboard Catalog contract (Phase 2,
// P2-01): a strongly typed, fully traceable mapping from the validated
// ObservabilityDocument and its GenerationPlan onto dashboard categories,
// items, signal references and panel capabilities.
//
// The Catalog is the single business input of every later Dashboard
// stage (Query Planner, Panels, Renderer, CLI). It never re-scans source
// code, never reads ASTs or Analyzer options, and never copies
// dependency target values (URLs, SQL text, Redis keys, Kafka payloads)
// that the Phase 1 security policy blocks. Each item keeps the source
// target ID, function ID, Metric/Span plan IDs and controlled
// low-cardinality labels so every query and panel can be traced back to
// a Phase 1 declaration.
//
// Ownership boundary: this package never imports compiler/goanalyzer,
// go/ast, go/types, go/packages, the plugin transport or any
// generated-document YAML contract. It consumes only the IR and
// GenerationPlan protobuf messages and the immutable DashboardPolicy
// (P2-03), which is resolved from the si.yaml `dashboard` node with
// defaults and CLI overrides before any builder runs.
//
// P2-04 adds the deterministic naming and layout layer: dashboard UIDs,
// panel/row IDs, refIds, safe titles and the 24-column grid. All of it is
// pure with respect to its inputs (ids.go, names.go, layout.go) and never
// depends on input order, the clock, the filesystem, global RNG or the
// environment; collisions are disambiguated by stable hash suffixes and
// reported as DASHBOARD_NAME_COLLISION diagnostics.
package dashboard

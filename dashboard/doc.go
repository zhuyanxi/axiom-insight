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
// GenerationPlan protobuf messages and the minimal immutable Dashboard
// Policy.
package dashboard

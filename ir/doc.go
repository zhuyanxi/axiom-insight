// Package ir contains the versioned Observability IR contract and adapters.
//
// # Analysis Facts vs Generation Plan ownership
//
// The IR has two distinct contracts, owned by two different producers:
//
//   - Analysis Facts (ir/v1/observability.proto) describe what the source
//     code statically proves: service identity, functions, endpoints,
//     dependencies and call edges, plus source diagnostics. They are
//     produced by the Analyzer (compiler/goanalyzer) during `si scan`.
//     The Analyzer never constructs, fills or reads a GenerationPlan.
//
//   - Generation Plan (ir/v1/generation.proto) describes instrumentation
//     decisions derived from the facts: which metrics to record, which
//     spans and structured log events to produce, and where each value
//     comes from. It is produced exclusively by the Generator Planner
//     (Phase 1) during `si generate`. The Planner and every downstream
//     Renderer consume only ir/v1 types and never import Analyzer or AST
//     packages.
//
// ObservabilityDocument carries the plan as an optional field
// (generation_plan, field 9). Because the Analyzer never sets it, scan
// output never contains plan content, and Phase 0 fixtures remain readable
// by Phase 1 types with the plan absent. The IR schema version and the
// GenerationPlan schema version evolve independently; every plan records
// both.
//
// Both contracts are append-only. Published field numbers and enum values
// must never be renamed, renumbered or reused; new capabilities require
// new fields and a new explicit schema version.
package ir

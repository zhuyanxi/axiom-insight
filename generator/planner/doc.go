// Package planner owns the deterministic Planner pipeline (Phase 1,
// P1-05): it validates the input IR, applies the generation Policy,
// coordinates the per-signal sub-planners and produces a fully referenced,
// deterministically sorted GenerationPlan.
//
// Fixed call chain:
//
//	Validate IR -> Plan (per enabled signal) -> Cross-check -> Sort
//	        -> Strict promotion -> Validate Plan
//
// Any fatal error stops the pipeline before a plan can be committed.
//
// Ownership boundary: this package imports only ir/v1 (the Analysis IR
// and GenerationPlan contracts), the generation Policy (generator/policy)
// and the naming/attribute policy (generator/naming). It never imports
// the Go analyzer, go/ast, go/types, go/packages, the plugin transport or
// the generated-document contracts, and it never reads source files, the
// working directory, environment variables, Git state or the network.
//
// Determinism: the plan's items, attributes, events, fields and
// diagnostics are sorted with stable comparators; sub-planner call order
// never influences the final ordering; the input ObservabilityDocument is
// never modified.
package planner

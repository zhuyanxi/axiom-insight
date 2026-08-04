// Package naming owns the unified naming, attribute-cardinality and
// privacy rules that every signal Planner applies before a GenerationPlan
// is built (Phase 1, P1-04).
//
// It provides three stateless policy surfaces:
//
//   - NamingPolicy: normalization of service/module/function/operation
//     names, deterministic Metric/Span/Log-event name construction and
//     stable name-collision disambiguation via target-ID derived SHA-256
//     suffixes.
//   - AttributePolicy: the metric/trace/logging attribute and field
//     allowlists, the unclosable credential denylist, sensitive-value
//     detection and field-key normalization.
//   - Cardinality: classic-exposition series estimation and the
//     instrument/series budget checks that fail with
//     GEN_CARDINALITY_LIMIT_EXCEEDED instead of silently truncating.
//
// Ownership boundary: this package never imports the Go analyzer, go/ast,
// go/types, go/packages, the plugin transport, the IR contract or the
// generated-document contracts. It depends only on the generation Policy
// (generator/policy) for message codes and limits. It reads no
// environment, Git state, filesystem or network; every function is pure
// and deterministic, and no function ever writes a rejected value into a
// diagnostic message.
package naming

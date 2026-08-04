// Package metrics implements the metrics signal sub-planners for the
// Planner pipeline (Phase 1, P1-06/P1-07): EndpointMetricsPlanner turns
// HTTP, gRPC and Cron endpoints into Counter, Histogram, optional Gauge
// and optional Summary plans.
//
// Rules: the EndpointKind mapping is exhaustive (unknown kinds produce
// GEN_UNSUPPORTED_ENTITY and are skipped, never guessed); every plan
// references its endpoint and carries the owning function ID; metric
// names, units and descriptions are built through the naming policy and
// never contain raw URLs, queries, source locations or schedule values;
// attribute vocabulary is exactly service, operation, status (gauges
// never carry status); estimated instruments and series are checked
// against the policy limits and any overflow fails the whole signal
// instead of truncating.
//
// Ownership boundary: this package never imports the Go analyzer, AST
// packages, the plugin transport, the IR contract or the
// generated-document contracts; it consumes only the validated IR, the
// generation Policy and the naming/attribute policy.
package metrics

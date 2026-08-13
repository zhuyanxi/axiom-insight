// Package pipeline implements P2-10: the deterministic assembly of the
// P2-06 overview, P2-07 HTTP/RPC, P2-08 Kafka and P2-09 dependency panel
// outputs into one immutable DashboardPlan, its render into canonical
// Grafana JSON bytes with pre- and post-render validation, and the
// definition count/hash report consumed by the P2-11 CLI.
//
// Rules pinned by the story:
//
//   - the plan combines P2-06..P2-09 outputs in the fixed CategoryOrder
//     (service overview first, then HTTP, RPC, Kafka, Database, Cache and
//     the client-call subsections); the renderer never re-infers
//     capability or rewrites query semantics;
//   - every dashboard holds at least one non-row panel; empty rows are
//     omitted and rows are stacked deterministically so no grid position
//     depends on input order;
//   - render validates the typed model before rendering and strictly
//     decodes and re-validates the JSON after rendering; any failure
//     returns DASHBOARD_RENDER_ERROR and no partial bytes;
//   - the output is the model package's canonical JSON (two-space
//     indentation, trailing LF, no HTML escaping, no timestamps) and
//     never carries the policy digest, absolute paths, hosts, users,
//     secrets or runtime IDs;
//   - the plan carries the source policy digest and aggregated sorted
//     diagnostics for the P2-11/P2-12 CLI report.
//
// The package never imports Analyzer/AST packages, never touches the
// network, the clock, the filesystem or the environment, and never embeds
// raw IR values into the dashboard.
package pipeline

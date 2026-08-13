// Package category implements P2-07: the HTTP and gRPC category rows.
// It consumes the P2-01 catalog, the P2-03 policy and the P2-05 query
// planner, plans one row per endpoint category, and renders them into the
// P2-02 model. BuildKafka (P2-08) and BuildDependencies (P2-09) extend
// the same Plan/Render contract to Kafka and to Database, Cache and
// client-dependency rows.
//
// Rules pinned by the story:
//
//   - HTTP Handler endpoints map to the HTTP row, gRPC Handler endpoints
//     to the RPC row; client dependencies are owned by P2-09 and never
//     enter these rows;
//   - each endpoint gets rate, error ratio, p50/p95/p99 duration and
//     in-flight panels gated on its catalog capabilities, plus one
//     operation breakdown table per row and controlled SERVER-span trace
//     links when available;
//   - titles, legends and queries use only the normalized Phase 1
//     operation; raw paths, gRPC metadata and high-cardinality values
//     never appear;
//   - rows carry a static description stating the Phase 1 Instrumentation
//     Plan origin and the runtime instrumentation requirement;
//   - a row is limited to MaxPanelsPerCategory panels and
//     MaxQueriesPerCategory queries; exceeding either fails with
//     DASHBOARD_PANEL_LIMIT_EXCEEDED, never truncates.
//
// The package never imports Analyzer/AST packages, never touches the
// network, the clock, the filesystem or the environment, and never embeds
// raw IR values into queries or display strings.
package category

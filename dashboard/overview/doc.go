// Package overview implements P2-06: the Service Overview row and its
// controlled variables. It consumes the P2-01 catalog and the P2-03
// policy, plans typed P2-05 queries per metric family, and renders the
// overview variables and panels into the P2-02 model.
//
// Rules pinned by the story:
//
//   - one datasource variable (name "datasource", type datasource, query
//     "prometheus", hide 0) and, only when the catalog declares at least
//     two valid low-cardinality operations, one custom operation variable
//     with static validated options in canonical order;
//   - rate, error ratio, p50/p95/p99 duration, in-flight and top-failing
//     panels are generated only when a metric family proves the capability;
//   - overview queries aggregate only items whose metric plan declares the
//     same metric name, type and label schema (a metric family); a
//     different name or label schema is never merged into one selector;
//   - an overview with no generatable panel is omitted and reported as
//     DASHBOARD_EMPTY_CATEGORY; the total overview query count is capped
//     at MaxOverviewQueries and exceeding it fails with
//     DASHBOARD_PANEL_LIMIT_EXCEEDED.
//
// The package never imports Analyzer/AST packages, never touches the
// network, the clock, the filesystem or the environment, and never embeds
// raw IR values into queries or display strings.
package overview

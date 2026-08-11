// Package query owns the Phase 2 PromQL and trace-link query planner
// (P2-05): a strongly typed, plan-driven query model that turns the
// validated Dashboard Catalog (P2-01), the resolved DashboardPolicy
// (P2-03) and the deterministic naming layer (P2-04) into reviewable
// queries.
//
// Every query is built from declared MetricPlan/SpanPlan identifiers and
// controlled values only. The package never accepts user PromQL, never
// concatenates external content into expressions, and never emits a
// query for a signal the catalog does not prove exists: unavailable
// capabilities become DASHBOARD_MISSING_REQUIRED_METRIC diagnostics and
// no substitute query.
//
// Ownership boundary: this package imports only the dashboard catalog
// package. It never imports compiler/goanalyzer, go/ast, go/types,
// go/packages, Prometheus or Grafana clients, the plugin transport or
// the generated-document YAML contract.
package query

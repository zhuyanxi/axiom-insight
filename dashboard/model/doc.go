// Package model owns the closed, versioned Grafana dashboard JSON v1
// contract (Phase 2, P2-02): typed model, strict decoder, semantic
// validator and canonical renderer.
//
// The model is a Grafana dashboard API/export subset pinned to Grafana
// Schema 41 with `schemaVersion: 41`, `id: null`, `version: 0` and
// `editable: true`. Panel types are limited to timeseries, stat, gauge,
// table and row; targets use the controlled `${datasource}` reference
// only; no `__inputs`, `__requires`, HTML, plugin types, arbitrary
// options passthrough, executable content or external datasource
// configuration is accepted.
//
// Contract closure: once `grafana.dashboard/v1` is published its field
// set is fixed. Adding, removing or renaming fields, or upgrading the
// Grafana schema, publishes a new explicit version; readers accept only
// the exact version they declare and reject unknown fields.
//
// Ownership boundary: this package never imports the analyzer, go/ast,
// go/types, go/packages, the plugin transport, the generator packages or
// the IR contract. It operates purely on JSON.
package model

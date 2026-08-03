package generator

// Package generator owns the three versioned, closed YAML contracts that
// `si generate` emits: metrics.yaml, otel.yaml and logging.yaml.
//
// Ownership boundary: this package never imports the Go analyzer, go/ast,
// go/types, go/packages or the plugin transport. It consumes fully formed
// document models and provides:
//
//   - strict YAML decoding (rejects unknown fields, duplicate keys, aliases,
//     anchors, timestamps and non-finite numbers),
//   - semantic validation (duplicate IDs/names, per-type constraints,
//     parent/status rules, redaction rules, correlation rules),
//   - deterministic rendering (typed field order, stable ID sorting, LF
//     endings, no anchors or aliases).
//
// Schema compatibility rule (closed contract): once a `generator.*/v1`
// schema is published its field set is fixed. Adding, removing or changing
// the meaning of any field or enum publishes a new explicit schema version;
// readers accept only the exact version they declare and reject unknown
// fields instead of ignoring them.

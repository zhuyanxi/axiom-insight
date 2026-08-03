// Package policy owns the `generation` configuration node of si.yaml: the
// strong-typed user input (GenerationConfig), the CLI override layer
// (Overrides), the immutable resolved configuration (Policy), merge rules
// (CLI flags > si.yaml > built-in defaults), field validation and the
// deterministic policy digest.
//
// Ownership boundary: this package never imports the Go analyzer, go/ast,
// go/types, go/packages, the plugin transport or the generated-document
// contracts. It consumes plain YAML bytes or already-parsed config values
// and produces a Policy that downstream Planner layers consume.
//
// Merge semantics: pointer fields (or nil slices) mean "absent" so an
// explicit false in si.yaml is never overwritten by a default true, and an
// explicit CLI flag always wins over the YAML value.
//
// Security rules: the loader performs no network requests, no shell
// execution and no template expansion; the built-in credential redaction
// denylist can never be disabled; error output carries field paths but
// never the full configuration content.
package policy

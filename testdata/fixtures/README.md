# Phase 0 Fixtures

Each directory is a self-contained Go module used by offline analyzer and CLI
end-to-end tests. `expected.json` stores stable semantic assertions: summary
counts, function identities, endpoints, dependencies, and diagnostic codes.
Absolute source paths and source locations stay out of snapshots.

Third-party fixture types live under a nested local module and are connected
with a `replace` directive. Fixture tests must never download modules or call
external services.

When adding or changing an endpoint or dependency rule:

1. Add or update one minimal positive fixture.
2. Add a negative case proving similarly named local code is not matched.
3. Update that fixture's `expected.json` with semantic fields only.
4. Run the focused fixture subtest and the full offline test suite.

# Dashboard v1 Fixture Index

Dashboard quality fixtures live here. Phase 1 protojson IR stays in
`testdata/generator/ir` and is reused by Dashboard catalog tests. This avoids
two copies of same source contract diverging.

Committed Dashboard outputs:

- `golden/composite/dashboard.json`
- `golden/no-trace-links/dashboard.json`
- `golden/no-client-dependencies/dashboard.json`
- `golden/degraded/dashboard.json`

Committed config fixtures (consumed by `dashboard.TestP213ConfigFixtures`):

- `config/default.yaml`
- `config/invalid-refresh.yaml`

`testdata/dashboard/corpus` holds Grafana Schema 41 compatibility fixtures.
See `docs/22-dashboard-fixtures.md` for update and contribution rules.
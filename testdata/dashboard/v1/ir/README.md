# IR Fixture Ownership

Dashboard tests reuse canonical Phase 1 protojson fixtures from
`testdata/generator/ir`:

- `composite.json`
- `dynamic-targets.json`
- `naming-collisions.json`
- `sensitive-values.json`
- `invalid-references.json`

`dashboard/pipeline.TestP213IRFixtureInventory` parses and routes each file
through Phase 1 planning and Dashboard Catalog validation.
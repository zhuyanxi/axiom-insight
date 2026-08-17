# Dashboard Fixture 与 Golden 规则（P2-13）

Phase 2 quality tests use offline fixtures only. Phase 1 protojson IR remains
canonical under `testdata/generator/ir`; Dashboard tests consume it through the
Catalog builder, so one IR fixture cannot drift from Phase 1 semantics.

## Fixture Map

| Concern | Canonical fixture or test |
| --- | --- |
| Composite IR and all entity kinds | `testdata/generator/ir/composite.json` |
| Dynamic targets | `testdata/generator/ir/dynamic-targets.json` |
| Naming collisions | `testdata/generator/ir/naming-collisions.json` |
| Sensitive values | `dashboard/pipeline.p213SensitiveDocument` and `TestP213SensitiveCanaryFullChain` |
| Invalid references | `testdata/generator/ir/invalid-references.json` |
| Dashboard config (valid + invalid refresh) | `testdata/dashboard/v1/config/default.yaml` and `invalid-refresh.yaml` |
| Grafana Schema 41 corpus | `testdata/dashboard/corpus/*.json` |
| Dashboard Goldens | `testdata/dashboard/v1/golden/*/dashboard.json` |

`testdata/dashboard/v1/` contains Dashboard-specific policy, report and
fixture metadata. It must not duplicate Phase 1 IR payloads.

## Golden Workflow

Normal tests never write snapshots. Regenerate Dashboard Goldens only with:

```sh
SI_UPDATE_GOLDEN=1 go test ./dashboard/pipeline -run TestP213DashboardGoldens -count=1
```

Review every changed dashboard path and run semantic diff output from
`dashboard/pipeline.Diff`. Golden updates require the implementation change,
fixture impact and expected semantic change in the same review.

## Contribution Rules

New entity kind, Metric/Span/label, policy field or schema field requires:

1. A focused fixture or documented reuse of an existing fixture.
2. Capability-matrix assertions and an error/degraded case.
3. A Golden update when rendered bytes change.
4. Sensitive-value canary coverage when new input fields can carry raw data.
5. Offline compatibility and permutation/determinism checks.

No test connects Grafana, Prometheus, Tempo or any remote service.
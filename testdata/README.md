# Test Fixtures

Store small, isolated source projects here for compiler and CLI tests.

Fixtures must remain local and deterministic. They must not require network
access or external services during tests.

Phase 0 endpoint and dependency fixtures live under `fixtures/`. Each fixture
has an `expected.json` semantic snapshot and can run independently, for
example:

```sh
go test ./cmd/si-cli -run 'TestScanFixtures/http$'
```

When changing a recognizer, update one positive fixture, one negative case,
and its snapshot. Keep absolute paths and source locations out of snapshots.
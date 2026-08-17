# Grafana Compatibility Ownership

Grafana Schema 41 compatibility payloads are committed under
`testdata/dashboard/corpus`. Dashboard model tests and
`dashboard/pipeline.TestP213CompatibilityCorpus` decode, validate and render
every corpus entry offline.
#!/usr/bin/env bash
# Phase 1 release check (P1-16): runs the non-interactive gates and
# records the release artifact hashes. Executable by a non-author.
# Never prints tokens or full environment contents.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-v0.2.0}"

echo "==> phase1-quality"
make phase1-quality

echo "==> performance budget"
SI_ENFORCE_PERF_BUDGET=1 go test ./generator/e2e/ -run TestPerformanceBudget1000Entities -count=1 -v

echo "==> build release binary (version ${VERSION})"
go build -ldflags "-X main.cliVersion=${VERSION}" -o bin/si ./cmd/si-cli

echo "==> version output"
./bin/si generate --version
./bin/si scan --version

echo "==> dry-run on a real fixture"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT
./bin/si generate testdata/fixtures/http --output-dir "$OUT/generate" --dry-run

echo "==> write verification + hashes"
./bin/si generate testdata/fixtures/http --output-dir "$OUT/generate"
shasum -a 256 "$OUT/generate/metrics.yaml" "$OUT/generate/otel.yaml" "$OUT/generate/logging.yaml"

echo "==> all release checks passed"

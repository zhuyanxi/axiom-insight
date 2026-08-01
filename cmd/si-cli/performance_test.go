package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

const (
	smallFixtureMaxDuration     = 5 * time.Second
	smallFixtureMaxAllocedBytes = 768 << 20
)

func BenchmarkScanSmallFixture(b *testing.B) {
	scanSmallFixture(b, filepath.Join(phase0FixturesRoot(b), "http"))
}

func TestScanSmallFixturePerformanceBudget(t *testing.T) {
	result := testing.Benchmark(func(b *testing.B) {
		scanSmallFixture(b, filepath.Join(phase0FixturesRoot(b), "http"))
	})
	if result.NsPerOp() > int64(smallFixtureMaxDuration) {
		t.Fatalf("small fixture scan took %s per operation, limit is %s", time.Duration(result.NsPerOp()), smallFixtureMaxDuration)
	}
	if result.AllocedBytesPerOp() > smallFixtureMaxAllocedBytes {
		t.Fatalf("small fixture scan allocated %d bytes per operation, limit is %d", result.AllocedBytesPerOp(), smallFixtureMaxAllocedBytes)
	}
}

func scanSmallFixture(b *testing.B, fixturePath string) {
	b.Helper()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"scan", fixturePath, "--format", "json"}, &stdout, &stderr); code != 0 {
			b.Fatalf("scan exit code = %d, want 0; stderr = %s", code, stderr.String())
		}
	}
}

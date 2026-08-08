package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkScanAndGenerateComposite measures the full CLI pipeline
// (scan -> plan -> render -> write) over the composite source fixture,
// with the Analyzer cost included. The four stages are reported
// separately by the existing micro-benchmarks (BenchmarkPlan,
// BenchmarkRender*, BenchmarkGenerateFromIR).
func BenchmarkScanAndGenerateComposite(b *testing.B) {
	root := writeCLIProject(b, map[string]string{
		"go.mod": "module example.com/bench-composite\n\ngo 1.26.1\n",
		"main.go": `package main

import (
	"database/sql"
	"net/http"
	"github.com/robfig/cron/v3"
)

func main() {
	http.HandleFunc("/orders/{id}", handleOrder)
	db, _ := sql.Open("postgres", "postgres://localhost/orders")
	c := cron.New()
	_, _ = c.AddFunc("0 3 * * *", cleanup)
	_ = db
	c.Start()
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	db, _ := sql.Open("postgres", "postgres://localhost/orders")
	rows, _ := db.Query("SELECT * FROM orders")
	_, _ = rows, w
}

func cleanup() {}
`,
	})
	outputDir := filepath.Join(b.TempDir(), "generate")
	args := []string{"generate", root, "--output-dir", outputDir, "--force"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var stdout, stderr bytes.Buffer
		code := run(args, &stdout, &stderr)
		if code != 0 {
			b.Fatalf("generate failed: %d %s", code, stderr.String())
		}
		// Remove the written files so each iteration writes from scratch.
		for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
			if err := os.Remove(filepath.Join(outputDir, name)); err != nil {
				b.Fatalf("cleanup: %v", err)
			}
		}
	}
}

package main

import (
	"bytes"
	"testing"
)

// BenchmarkP214DashboardScanToWrite measures the offline CLI path from Go
// source scan through Dashboard plan, render/validate, and atomic write.
// The 1,000-item pipeline benchmark lives in dashboard/pipeline; this
// benchmark captures the separate source-analysis and filesystem cost.
func BenchmarkP214DashboardScanToWrite(b *testing.B) {
	root := writeCLIProject(b, map[string]string{
		"go.mod": "module example.com/bench-dashboard\n\ngo 1.26.1\n",
		"main.go": `package main

import "net/http"

func main() {
	http.HandleFunc("/orders", handleOrders)
}

func handleOrders(writer http.ResponseWriter, request *http.Request) {
	writer.WriteHeader(http.StatusNoContent)
}
`,
	})
	outputDir := b.TempDir() + "/dashboards"
	args := []string{"dashboard", root, "--output-dir", outputDir, "--force"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 0 {
			b.Fatalf("dashboard failed: code=%d stderr=%s", code, stderr.String())
		}
	}
}

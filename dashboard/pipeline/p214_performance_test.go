package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

const (
	p214CatalogItems = 1000
)

type p214PerformanceBaseline struct {
	Items             int     `json:"items"`
	MaxNanoseconds    int64   `json:"max_nanoseconds"`
	MaxAllocatedBytes uint64  `json:"max_allocated_bytes"`
	Tolerance         float64 `json:"tolerance"`
}

func loadP214PerformanceBaseline(t testing.TB) p214PerformanceBaseline {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "testdata", "dashboard", "v1", "perf", "baseline.json"))
	if err != nil {
		t.Fatalf("read P2-14 performance baseline: %v", err)
	}
	var baseline p214PerformanceBaseline
	if err := json.Unmarshal(contents, &baseline); err != nil {
		t.Fatalf("parse P2-14 performance baseline: %v", err)
	}
	if baseline.Items != p214CatalogItems || baseline.MaxNanoseconds <= 0 || baseline.MaxAllocatedBytes == 0 || baseline.Tolerance < 0 {
		t.Fatalf("invalid P2-14 performance baseline: %+v", baseline)
	}
	return baseline
}

// p214PerformancePolicy raises only the configured ceilings to their v1 hard
// limits. The production defaults remain unchanged; the fixture is scoped to
// overview aggregation so category-level safety limits are not bypassed.
func p214PerformancePolicy(t testing.TB) dashboard.DashboardPolicy {
	t.Helper()
	maxPanels := int64(dashboard.HardMaxPanels)
	maxQueries := int64(dashboard.HardMaxQueries)
	policy, err := dashboard.Resolve(&dashboard.DashboardConfig{
		MaxPanels:         &maxPanels,
		MaxQueries:        &maxQueries,
		IncludeTraceLinks: boolPointer(false),
	}, nil)
	if err != nil {
		t.Fatalf("resolve performance policy: %v", err)
	}
	return *policy
}

func boolPointer(value bool) *bool { return &value }

func p214CatalogFixture() *dashboard.DashboardCatalog {
	items := make([]dashboard.DashboardItem, 0, p214CatalogItems)
	for index := 0; index < p214CatalogItems; index++ {
		itemID := "item:overview:" + zeroPad(index)
		items = append(items, dashboard.DashboardItem{
			ID:        itemID,
			Category:  dashboard.CategoryServiceOverview,
			Operation: "operation-" + zeroPad(index),
			Target:    dashboard.TargetRef{Kind: "endpoint", ID: itemID},
			Metrics: []dashboard.SignalReference{
				{PlanID: "metric:count:" + zeroPad(index), Name: "requests_total", Type: "counter", Attributes: []string{"service", "operation", "status"}},
				{PlanID: "metric:duration:" + zeroPad(index), Name: "request_duration", Type: "histogram", Attributes: []string{"service"}},
				{PlanID: "metric:inflight:" + zeroPad(index), Name: "in_flight", Type: "gauge", Attributes: []string{"service"}},
			},
		})
	}
	return &dashboard.DashboardCatalog{
		SchemaVersion:               dashboard.CatalogSchemaVersion,
		SourceIRSchemaVersion:       dashboard.SupportedIRSchemaVersion,
		GenerationPlanSchemaVersion: "generation_plan/v1",
		ServiceName:                 "performance",
		Items:                       items,
	}
}

func zeroPad(value int) string {
	return fmt.Sprintf("%04d", value)
}

func benchmarkP214BuildAndRender(t testing.TB, catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) {
	t.Helper()
	plan, err := Build(catalog, policy)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := Render(plan); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

// TestP214CanonicalOutput pins the cross-platform byte contract for the
// committed composite fixture: valid JSON, LF-only canonical bytes, and a
// stable hash independent of host path, clock, or newline conventions.
func TestP214CanonicalOutput(t *testing.T) {
	result, err := Render(mustBuildP214(t, fullCatalog(), resolvePolicy(t)))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !json.Valid(result.Bytes) {
		t.Fatal("canonical dashboard is not valid JSON")
	}
	if bytes.Contains(result.Bytes, []byte("\r")) || !bytes.HasSuffix(result.Bytes, []byte("\n")) || bytes.HasSuffix(result.Bytes, []byte("\n\n")) {
		t.Fatal("canonical dashboard must use one LF terminator and no CR bytes")
	}
	if result.SHA256 != "5adebef998f8ce78f2ee8755265ab159b1e59aa4cee52f42b942acbab8cf061b" {
		t.Fatalf("composite canonical hash = %s", result.SHA256)
	}
}

func mustBuildP214(t testing.TB, catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) *Plan {
	t.Helper()
	plan, err := Build(catalog, policy)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return plan
}

// BenchmarkP214Dashboard1000 reports catalog construction, query planning and
// layout, render/validate, and the combined dashboard pipeline separately.
func BenchmarkP214Dashboard1000(b *testing.B) {
	policy := p214PerformancePolicy(b)
	catalog := p214CatalogFixture()

	b.Run("catalog", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = p214CatalogFixture()
		}
	})
	b.Run("plan-layout", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			plan, err := Build(catalog, policy)
			if err != nil {
				b.Fatal(err)
			}
			_ = plan.Rows()
		}
	})
	plan, err := Build(catalog, policy)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("render-validate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Render(plan); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("catalog-plan-render", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchmarkP214BuildAndRender(b, catalog, policy)
		}
	})
}

// TestP214DashboardPerformanceBudget1000 enforces the approved reference
// budget only when SI_ENFORCE_PERF_BUDGET=1 is explicitly set.
func TestP214DashboardPerformanceBudget1000(t *testing.T) {
	if os.Getenv("SI_ENFORCE_PERF_BUDGET") == "" {
		t.Skip("performance budget enforcement is opt-in (SI_ENFORCE_PERF_BUDGET=1)")
	}
	baseline := loadP214PerformanceBaseline(t)
	policy := p214PerformancePolicy(t)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			benchmarkP214BuildAndRender(b, p214CatalogFixture(), policy)
		}
	})
	allowedNanoseconds := int64(float64(baseline.MaxNanoseconds) * (1 + baseline.Tolerance))
	allowedAllocations := uint64(float64(baseline.MaxAllocatedBytes) * (1 + baseline.Tolerance))
	if result.NsPerOp() > allowedNanoseconds || uint64(result.AllocedBytesPerOp()) > allowedAllocations {
		t.Fatalf("P2-14 performance budget exceeded: items=%d elapsed=%s allocated=%d bytes (baseline elapsed<%s allocations<%d bytes tolerance=%.0f%%)",
			p214CatalogItems, time.Duration(result.NsPerOp()), result.AllocedBytesPerOp(),
			time.Duration(baseline.MaxNanoseconds), baseline.MaxAllocatedBytes, baseline.Tolerance*100)
	}
}

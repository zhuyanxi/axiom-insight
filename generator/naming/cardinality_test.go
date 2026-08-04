package naming

import (
	"strings"
	"testing"
)

func TestEstimateSeriesFormulas(t *testing.T) {
	estimator := SeriesEstimator{}
	tests := []struct {
		name       string
		metricType string
		attributes []int
		buckets    int
		quantiles  int
		want       int64
	}{
		{"counter single combo", "counter", []int{1, 1, 5}, 0, 0, 5},
		{"gauge without status", "gauge", []int{1, 1}, 0, 0, 1},
		{"histogram formula", "histogram", []int{1, 1, 5}, 11, 0, 5 * 14},
		{"histogram one combo", "histogram", []int{1}, 3, 0, 6},
		{"summary formula", "summary", []int{1, 5}, 0, 3, 5 * 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := estimator.EstimateSeries(test.metricType, test.attributes, test.buckets, test.quantiles)
			if err != nil {
				t.Fatalf("EstimateSeries failed: %v", err)
			}
			if got != test.want {
				t.Errorf("EstimateSeries = %d, want %d", got, test.want)
			}
		})
	}
}

func TestEstimateSeriesRejectsInvalidInput(t *testing.T) {
	estimator := SeriesEstimator{}
	tests := []struct {
		name       string
		metricType string
		attributes []int
		buckets    int
		quantiles  int
	}{
		{"unknown type", "thermometer", []int{1}, 0, 0},
		{"zero attribute cardinality", "counter", []int{0}, 0, 0},
		{"histogram without buckets", "histogram", []int{1}, 0, 0},
		{"summary without quantiles", "summary", []int{1}, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := estimator.EstimateSeries(test.metricType, test.attributes, test.buckets, test.quantiles); err == nil {
				t.Errorf("EstimateSeries(%s) must fail", test.metricType)
			}
		})
	}
}

// TestSeriesBudgetAC7: an estimate above the configured limit fails with
// GEN_CARDINALITY_LIMIT_EXCEEDED, reports the estimate, limit and signal,
// and never truncates.
func TestSeriesBudgetAC7(t *testing.T) {
	check := BudgetCheck{}
	err := check.SeriesBudget("metrics", 75, 50)
	if err == nil {
		t.Fatal("SeriesBudget must fail over the limit")
	}
	message := err.Error()
	for _, fragment := range []string{"GEN_CARDINALITY_LIMIT_EXCEEDED", "metrics", "75", "50", "refusing to truncate"} {
		if !strings.Contains(message, fragment) {
			t.Errorf("error %q lacks %q", message, fragment)
		}
	}
	if strings.Contains(message, "metrics.yaml") || strings.Contains(message, "plan") && strings.Contains(message, "content") {
		t.Errorf("error carries plan content: %q", message)
	}
}

func TestBudgetWithinLimitsPasses(t *testing.T) {
	check := BudgetCheck{}
	if err := check.SeriesBudget("metrics", 50, 50); err != nil {
		t.Errorf("at-limit estimate must pass: %v", err)
	}
	if err := check.InstrumentBudget("metrics", 10000, 10000); err != nil {
		t.Errorf("at-limit instrument count must pass: %v", err)
	}
}

func TestInstrumentBudgetFailsOverLimit(t *testing.T) {
	check := BudgetCheck{}
	err := check.InstrumentBudget("metrics", 10001, 10000)
	if err == nil || !strings.Contains(err.Error(), "GEN_CARDINALITY_LIMIT_EXCEEDED") {
		t.Fatalf("InstrumentBudget must fail with the cardinality code, got %v", err)
	}
}

// TestFullPipelineEstimateThenBudget: the composite fixture from AC7 —
// histogram with 11 buckets, status domain 5, service and operation
// constant — yields 70 series and fails a budget of 50.
func TestFullPipelineEstimateThenBudget(t *testing.T) {
	estimator := SeriesEstimator{}
	estimated, err := estimator.EstimateSeries("histogram", []int{1, 1, 5}, 11, 0)
	if err != nil {
		t.Fatalf("EstimateSeries failed: %v", err)
	}
	if estimated != 70 {
		t.Fatalf("estimate = %d, want 70", estimated)
	}
	check := BudgetCheck{}
	if err := check.SeriesBudget("metrics", estimated, 100); err != nil {
		t.Errorf("budget 100 must pass for 70 series: %v", err)
	}
	if err := check.SeriesBudget("metrics", estimated, 50); err == nil {
		t.Error("budget 50 must fail for 70 series")
	}
}

// TestStrictErrorAC6: strict mode promotes warnings; non-strict keeps
// them; error-level codes fail regardless.
func TestStrictErrorAC6(t *testing.T) {
	warnings := []Diagnostic{
		{Code: "GEN_NAME_COLLISION", Signal: "metrics", TargetID: "dep:a", Field: "name", Message: "collision disambiguated"},
	}
	if err := StrictError(false, warnings); err != nil {
		t.Errorf("non-strict mode must keep warnings as report items: %v", err)
	}
	if err := StrictError(true, warnings); err == nil {
		t.Fatal("strict mode must promote warnings to failure")
	}
	if err := StrictError(false, nil); err != nil {
		t.Errorf("no diagnostics must never fail: %v", err)
	}
	errors := []Diagnostic{
		{Code: "GEN_CARDINALITY_LIMIT_EXCEEDED", Signal: "metrics", TargetID: "dep:a", Field: "series", Message: "budget exceeded"},
	}
	if err := StrictError(false, errors); err == nil {
		t.Fatal("error-level diagnostics must fail even in non-strict mode")
	}
}

func TestIsWarning(t *testing.T) {
	for _, code := range []string{
		"GEN_NAME_COLLISION", "GEN_CARDINALITY_BLOCKED", "GEN_SENSITIVE_VALUE_DROPPED",
		"GEN_UNSUPPORTED_ENTITY", "GEN_INCOMPLETE_TARGET",
	} {
		if !IsWarning(code) {
			t.Errorf("IsWarning(%s) = false", code)
		}
	}
	for _, code := range []string{"GEN_CARDINALITY_LIMIT_EXCEEDED", "GEN_INVALID_CONFIG", "GEN_RENDER_ERROR"} {
		if IsWarning(code) {
			t.Errorf("IsWarning(%s) = true", code)
		}
	}
}

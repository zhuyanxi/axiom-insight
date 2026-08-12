package query

import (
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

func overviewItem(id, operation string, metrics ...dashboard.SignalReference) dashboard.DashboardItem {
	return dashboard.DashboardItem{
		ID: id, Category: dashboard.CategoryHTTP, Operation: operation, Metrics: metrics,
	}
}

func overviewMetric(planID, name, metricType string, attributes ...string) dashboard.SignalReference {
	return dashboard.SignalReference{
		PlanID: planID, Name: name, Type: metricType, Attributes: attributes,
	}
}

func serviceMatcher(value string) LabelMatcher {
	return LabelMatcher{Label: "service", Op: MatchEqual, Value: value}
}

func overviewItems() []dashboard.DashboardItem {
	return []dashboard.DashboardItem{
		overviewItem("ep:http:a", "get",
			overviewMetric("m_a", "http_requests_total", "counter", "service", "operation", "status"),
			overviewMetric("h_a", "http_request_duration", "histogram", "service", "operation"),
			overviewMetric("g_a", "http_in_flight", "gauge", "service", "operation")),
		overviewItem("ep:http:b", "post",
			overviewMetric("m_b", "http_requests_total", "counter", "service", "operation", "status"),
			overviewMetric("h_b", "http_request_duration", "histogram", "service", "operation"),
			overviewMetric("g_b", "http_in_flight", "gauge", "service", "operation")),
	}
}

func overviewRatePlan(itemIDs ...string) *QueryPlan {
	expression := &Aggregation{
		By: []string{"operation"},
		Expr: &RateExpression{
			Selector: &MetricSelector{
				MetricName: "http_requests_total",
				Matchers:   []LabelMatcher{serviceMatcher("payment")},
			},
			Interval: dashboard.DefaultRateInterval,
		},
	}
	return &QueryPlan{
		CanonicalKey: "query:rate:overview:http_requests_total",
		Kind:         QueryKindRate,
		ItemID:       "overview",
		Purpose:      "rate",
		PlanIDs:      []string{"m_a", "m_b"},
		Expression:   expression,
	}
}

// TestValidateOverviewPlanAC1 pins the cross-item rules: a query sharing
// one metric name, type and label schema across every aggregated item is
// valid, and drift is rejected.
func TestValidateOverviewPlanAC1(t *testing.T) {
	items := overviewItems()
	if violations := ValidateOverviewPlan(overviewRatePlan(), items, "payment"); len(violations) != 0 {
		t.Fatalf("valid overview rate rejected: %v", violations)
	}

	t.Run("operation matcher rejected", func(t *testing.T) {
		plan := overviewRatePlan()
		plan.Expression.(*Aggregation).Expr.(*RateExpression).Selector.Matchers = append(
			plan.Expression.(*Aggregation).Expr.(*RateExpression).Selector.Matchers,
			LabelMatcher{Label: "operation", Op: MatchEqual, Value: "get"},
		)
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "must not filter by operation") {
			t.Errorf("expected operation matcher rejection, got %v", violations)
		}
	})

	t.Run("metric missing from one item", func(t *testing.T) {
		items := overviewItems()
		items[1].Metrics[0].Name = "http_requests_total_other"
		if violations := ValidateOverviewPlan(overviewRatePlan(), items, "payment"); !containsMessage(violations, "differs across aggregated items") {
			t.Errorf("expected schema drift rejection, got %v", violations)
		}
	})

	t.Run("label schema differs", func(t *testing.T) {
		items := overviewItems()
		items[1].Metrics[0].Attributes = []string{"service", "operation"}
		if violations := ValidateOverviewPlan(overviewRatePlan(), items, "payment"); !containsMessage(violations, "label schema differs") {
			t.Errorf("expected schema drift rejection, got %v", violations)
		}
	})

	t.Run("undeclared plan id", func(t *testing.T) {
		plan := overviewRatePlan()
		plan.PlanIDs = []string{"m_a", "m_missing"}
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "undeclared plan ID") {
			t.Errorf("expected undeclared plan ID rejection, got %v", violations)
		}
	})

	t.Run("plan ids do not cover selected metric", func(t *testing.T) {
		plan := overviewRatePlan()
		plan.PlanIDs = []string{"g_a", "g_b"}
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "do not cover a selected metric") {
			t.Errorf("expected plan ID coverage rejection, got %v", violations)
		}
	})
}

func TestValidateOverviewPlanKinds(t *testing.T) {
	items := overviewItems()

	t.Run("error ratio", func(t *testing.T) {
		numerator := &MetricSelector{
			MetricName: "http_requests_total",
			Matchers: []LabelMatcher{
				serviceMatcher("payment"),
				{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern},
			},
		}
		denominator := &MetricSelector{
			MetricName: "http_requests_total",
			Matchers:   []LabelMatcher{serviceMatcher("payment")},
		}
		plan := &QueryPlan{
			CanonicalKey: "query:error_ratio:overview:http_requests_total",
			Kind:         QueryKindErrorRatio,
			ItemID:       "overview",
			Purpose:      "error_ratio",
			PlanIDs:      []string{"m_a", "m_b"},
			Expression: &BinaryExpression{
				Op: BinaryDivide,
				Left: &Aggregation{
					By:   []string{"operation"},
					Expr: &RateExpression{Selector: numerator, Interval: dashboard.DefaultRateInterval},
				},
				Right: &Aggregation{
					By:   []string{"operation"},
					Expr: &RateExpression{Selector: denominator, Interval: dashboard.DefaultRateInterval},
				},
			},
		}
		if violations := ValidateOverviewPlan(plan, items, "payment"); len(violations) != 0 {
			t.Fatalf("valid error ratio rejected: %v", violations)
		}
	})

	t.Run("error ratio domain mismatch", func(t *testing.T) {
		plan := &QueryPlan{
			CanonicalKey: "query:error_ratio:overview:http_requests_total",
			Kind:         QueryKindErrorRatio,
			ItemID:       "overview",
			Purpose:      "error_ratio",
			PlanIDs:      []string{"m_a", "m_b"},
			Expression: &BinaryExpression{
				Op: BinaryDivide,
				Left: &Aggregation{
					By: []string{"operation"},
					Expr: &RateExpression{
						Selector: &MetricSelector{MetricName: "http_requests_total", Matchers: []LabelMatcher{serviceMatcher("payment")}},
						Interval: dashboard.DefaultRateInterval,
					},
				},
				Right: &Aggregation{
					By: []string{"operation"},
					Expr: &RateExpression{
						Selector: &MetricSelector{MetricName: "rpc_requests_total", Matchers: []LabelMatcher{serviceMatcher("payment")}},
						Interval: dashboard.DefaultRateInterval,
					},
				},
			},
		}
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "must share the same metric") {
			t.Errorf("expected domain mismatch rejection, got %v", violations)
		}
	})

	t.Run("gauge rate rejected", func(t *testing.T) {
		plan := &QueryPlan{
			CanonicalKey: "query:rate:overview:http_in_flight",
			Kind:         QueryKindRate,
			ItemID:       "overview",
			Purpose:      "rate",
			PlanIDs:      []string{"g_a", "g_b"},
			Expression: &Aggregation{
				By: []string{"operation"},
				Expr: &RateExpression{
					Selector: &MetricSelector{MetricName: "http_in_flight", Matchers: []LabelMatcher{serviceMatcher("payment")}},
					Interval: dashboard.DefaultRateInterval,
				},
			},
		}
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "rate() is not allowed on a gauge") {
			t.Errorf("expected gauge rate rejection, got %v", violations)
		}
	})

	t.Run("top failing", func(t *testing.T) {
		plan := &QueryPlan{
			CanonicalKey: "query:top_failing:overview:http_requests_total",
			Kind:         QueryKindTopFailing,
			ItemID:       "overview",
			Purpose:      "top_failing",
			PlanIDs:      []string{"m_a", "m_b"},
			Expression: &Aggregation{
				By: []string{"operation"},
				Expr: &RateExpression{
					Selector: &MetricSelector{
						MetricName: "http_requests_total",
						Matchers: []LabelMatcher{
							serviceMatcher("payment"),
							{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern},
						},
					},
					Interval: dashboard.DefaultRateInterval,
				},
			},
		}
		if violations := ValidateOverviewPlan(plan, items, "payment"); len(violations) != 0 {
			t.Fatalf("valid top failing rejected: %v", violations)
		}
		plan.Expression.(*Aggregation).Expr.(*RateExpression).Selector.Matchers[1].Value = `.*`
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "raw regexes are not allowed") {
			t.Errorf("expected raw regex rejection, got %v", violations)
		}
	})

	t.Run("in flight", func(t *testing.T) {
		plan := &QueryPlan{
			CanonicalKey: "query:in_flight:overview:http_in_flight",
			Kind:         QueryKindInFlight,
			ItemID:       "overview",
			Purpose:      "in_flight",
			PlanIDs:      []string{"g_a", "g_b"},
			Expression: &Aggregation{
				By: []string{"operation"},
				Expr: &MetricSelector{
					MetricName: "http_in_flight",
					Matchers:   []LabelMatcher{serviceMatcher("payment")},
				},
			},
		}
		if violations := ValidateOverviewPlan(plan, items, "payment"); len(violations) != 0 {
			t.Fatalf("valid in-flight rejected: %v", violations)
		}
		plan.Expression.(*Aggregation).Expr.(*MetricSelector).Matchers = append(
			plan.Expression.(*Aggregation).Expr.(*MetricSelector).Matchers,
			LabelMatcher{Label: "status", Op: MatchEqual, Value: "ok"},
		)
		if violations := ValidateOverviewPlan(plan, items, "payment"); !containsMessage(violations, "must not filter by status") {
			t.Errorf("expected in-flight status rejection, got %v", violations)
		}
	})
}

func containsMessage(violations []*ValidationError, wanted string) bool {
	for _, violation := range violations {
		if strings.Contains(violation.Message, wanted) {
			return true
		}
	}
	return false
}

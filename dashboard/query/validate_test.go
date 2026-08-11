package query

import (
	"strings"
	"testing"
)

// TestValidatePlansClean runs the validator over every generated plan:
// plans built by the planner are always valid (internal consistency).
func TestValidatePlansClean(t *testing.T) {
	item := fullItem()
	plans, _, diagnostics := PlanItemQueries(item, "payment", resolvePolicy(t))
	if len(diagnostics) != 0 {
		t.Fatalf("fixture produced diagnostics: %v", diagnostics)
	}
	for _, plan := range plans {
		if violations := ValidatePlan(&plan, item, "payment"); len(violations) != 0 {
			t.Errorf("plan %s invalid: %v", plan.CanonicalKey, violations)
		}
	}
}

// TestValidateCrossCheck is the cross-check requirement: any drift between
// the query and the declared metric plans fails validation.
func TestValidateCrossCheck(t *testing.T) {
	item := fullItem()

	cases := []struct {
		name   string
		mutate func(*QueryPlan)
		field  string
	}{
		{
			name: "renamed metric",
			mutate: func(plan *QueryPlan) {
				replaceSelectors(plan.Expression, func(selector *MetricSelector) {
					selector.MetricName = "http_requests_total_changed"
				})
			},
			field: "selector.metric",
		},
		{
			name: "undeclared attribute matcher",
			mutate: func(plan *QueryPlan) {
				replaceSelectors(plan.Expression, func(selector *MetricSelector) {
					selector.Matchers = append(selector.Matchers, LabelMatcher{Label: "status", Op: MatchEqual, Value: "ok"})
				})
			},
			field: "status",
		},
		{
			name: "wrong service value",
			mutate: func(plan *QueryPlan) {
				replaceSelectors(plan.Expression, func(selector *MetricSelector) {
					for index := range selector.Matchers {
						if selector.Matchers[index].Label == "service" {
							selector.Matchers[index].Value = "other-service"
						}
					}
				})
			},
			field: "service",
		},
		{
			name: "wrong interval",
			mutate: func(plan *QueryPlan) {
				replaceRates(plan.Expression, func(rate *RateExpression) {
					rate.Interval = "$__interval"
				})
			},
			field: "interval",
		},
		{
			name: "undeclared plan id",
			mutate: func(plan *QueryPlan) {
				plan.PlanIDs = []string{"m_ghost"}
			},
			field: "plan_ids",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			// The rate plan is the mutation base: single selector, single
			// rate, no ratio-domain checks shadowing the mutated field.
			// Plans are rebuilt per case because expressions are shared
			// pointers.
			plans, _, _ := PlanItemQueries(item, "payment", resolvePolicy(t))
			var base QueryPlan
			for _, plan := range plans {
				if plan.Kind == QueryKindRate {
					base = plan
					break
				}
			}
			if base.CanonicalKey == "" {
				t.Fatal("no rate plan in fixture")
			}
			test.mutate(&base)
			violations := ValidatePlan(&base, item, "payment")
			if len(violations) == 0 {
				t.Fatal("mutation must fail validation")
			}
			if !strings.Contains(violations[0].Error(), test.field) {
				t.Errorf("expected field %q in %v", test.field, violations[0])
			}
		})
	}
}

// TestValidateBareSelector rejects a bare selector root.
func TestValidateBareSelector(t *testing.T) {
	item := fullItem()
	plan := QueryPlan{
		CanonicalKey: "query:rate:item:x:rate", Kind: QueryKindRate, ItemID: item.ID,
		Expression: &MetricSelector{MetricName: "http_requests_total"},
	}
	violations := ValidatePlan(&plan, item, "payment")
	found := false
	for _, violation := range violations {
		if strings.Contains(violation.Message, "bare") {
			found = true
		}
	}
	if !found {
		t.Errorf("bare selector root must fail validation, got %v", violations)
	}
}

// TestValidateRawRegex rejects any regex beyond the fixed error-status
// pattern, including on the denominator and in-flight queries.
func TestValidateRawRegex(t *testing.T) {
	item := fullItem()
	plan := QueryPlan{
		CanonicalKey: "query:rate:item:x:rate", Kind: QueryKindRate, ItemID: item.ID,
		Expression: &Aggregation{By: []string{"operation"}, Expr: &RateExpression{
			Selector: &MetricSelector{MetricName: "http_requests_total", Matchers: []LabelMatcher{
				{Label: "service", Op: MatchEqual, Value: "payment"},
				{Label: "operation", Op: MatchRegex, Value: "g.*"},
			}},
			Interval: "$__rate_interval",
		}},
	}
	for _, violation := range ValidatePlan(&plan, item, "payment") {
		if strings.Contains(violation.Message, "raw regex") {
			return
		}
	}
	t.Fatalf("raw regex must be rejected")
}

// TestValidateInFlightRules forbids rate() and status matchers for gauge
// queries.
func TestValidateInFlightRules(t *testing.T) {
	item := fullItem()
	cases := []struct {
		name  string
		expr  Expression
		field string
	}{
		{
			name: "rated gauge",
			expr: &Aggregation{By: []string{"operation"}, Expr: &RateExpression{
				Selector: &MetricSelector{MetricName: "http_in_flight"}, Interval: "$__rate_interval",
			}},
			field: "rate()",
		},
		{
			name: "status matcher",
			expr: &Aggregation{By: []string{"operation"}, Expr: &MetricSelector{
				MetricName: "http_in_flight",
				Matchers:   []LabelMatcher{{Label: "status", Op: MatchEqual, Value: "ok"}},
			}},
			field: "status",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := QueryPlan{
				CanonicalKey: "query:in_flight:item:x:in_flight", Kind: QueryKindInFlight, ItemID: item.ID,
				Expression: test.expr,
			}
			for _, violation := range ValidatePlan(&plan, item, "payment") {
				if strings.Contains(violation.Error(), test.field) {
					return
				}
			}
			t.Fatalf("expected %q violation, got none", test.field)
		})
	}
}

// TestValidateErrorRatioSides is AC5 at the validator level: numerator
// and denominator must share metric/domain matchers, and the numerator
// may only add the fixed error-status matcher.
func TestValidateErrorRatioSides(t *testing.T) {
	item := fullItem()
	buildRatio := func(numerator, denominator *MetricSelector) *BinaryExpression {
		return &BinaryExpression{Op: BinaryDivide,
			Left:  &Aggregation{By: []string{"operation"}, Expr: &RateExpression{Selector: numerator, Interval: "$__rate_interval"}},
			Right: &Aggregation{By: []string{"operation"}, Expr: &RateExpression{Selector: denominator, Interval: "$__rate_interval"}},
		}
	}
	domain := []LabelMatcher{
		{Label: "service", Op: MatchEqual, Value: "payment"},
		{Label: "operation", Op: MatchEqual, Value: "get"},
	}
	t.Run("valid ratio passes", func(t *testing.T) {
		plan := QueryPlan{
			CanonicalKey: "query:error_ratio:item:x:error_ratio", Kind: QueryKindErrorRatio, ItemID: item.ID,
			Expression: buildRatio(
				&MetricSelector{MetricName: "http_requests_total", Matchers: append(domain, LabelMatcher{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern})},
				&MetricSelector{MetricName: "http_requests_total", Matchers: domain},
			),
		}
		if violations := ValidatePlan(&plan, item, "payment"); len(violations) != 0 {
			t.Fatalf("valid ratio failed: %v", violations)
		}
	})
	t.Run("different metric rejected", func(t *testing.T) {
		plan := QueryPlan{
			CanonicalKey: "query:error_ratio:item:x:error_ratio", Kind: QueryKindErrorRatio, ItemID: item.ID,
			Expression: buildRatio(
				&MetricSelector{MetricName: "http_requests_total", Matchers: append(domain, LabelMatcher{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern})},
				&MetricSelector{MetricName: "other_metric", Matchers: domain},
			),
		}
		if violations := ValidatePlan(&plan, item, "payment"); len(violations) == 0 {
			t.Fatal("different denominator metric must fail")
		}
	})
	t.Run("status on denominator rejected", func(t *testing.T) {
		plan := QueryPlan{
			CanonicalKey: "query:error_ratio:item:x:error_ratio", Kind: QueryKindErrorRatio, ItemID: item.ID,
			Expression: buildRatio(
				&MetricSelector{MetricName: "http_requests_total", Matchers: domain},
				&MetricSelector{MetricName: "http_requests_total", Matchers: append(domain, LabelMatcher{Label: "status", Op: MatchEqual, Value: "ok"})},
			),
		}
		found := false
		for _, violation := range ValidatePlan(&plan, item, "payment") {
			if strings.Contains(violation.Message, "numerator") {
				found = true
			}
		}
		if !found {
			t.Fatal("status matcher on the denominator must be rejected")
		}
	})
}

// TestValidateInjectionValues: a parsed matcher value carrying PromQL
// syntax stays inert string content — the validator rejects it before it
// can reach a renderer output.
func TestValidateInjectionValues(t *testing.T) {
	item := fullItem()
	// "x} or on(y) (z)" parses as a quoted value; validation must block it.
	expression, err := Parse(`requests_total{service="x} or on(y) (z)"}`)
	if err != nil {
		t.Fatalf("reference expression must parse: %v", err)
	}
	plan := QueryPlan{
		CanonicalKey: "query:rate:item:x:rate", Kind: QueryKindRate, ItemID: item.ID,
		Expression: &Aggregation{By: []string{"operation"}, Expr: &RateExpression{
			Selector: expression.(*MetricSelector), Interval: "$__rate_interval",
		}},
	}
	for _, violation := range ValidatePlan(&plan, item, "payment") {
		if strings.Contains(violation.Message, "controlled value") {
			return
		}
	}
	t.Fatalf("injected matcher value must fail validation")
}

// TestValidateTraceLink rules: fixed datasource, controlled names, plan
// IDs declared.
func TestValidateTraceLink(t *testing.T) {
	item := fullItem()
	policy := resolvePolicy(t)
	link := TraceLinkPlan{
		CanonicalKey: "trace:item:http:get_user", ItemID: item.ID,
		PlanIDs: []string{"s_span"}, DatasourceVariable: policy.DatasourceVariableName,
		ServiceName: "payment", Operation: "get", SpanName: "GET /users",
	}
	if violations := ValidateTraceLink(&link, item, policy); len(violations) != 0 {
		t.Fatalf("valid link failed: %v", violations)
	}

	broken := link
	broken.DatasourceVariable = "my_ds"
	broken.SpanName = "GET /\x00"
	broken.PlanIDs = []string{"s_ghost"}
	violations := ValidateTraceLink(&broken, item, policy)
	if len(violations) < 3 {
		t.Errorf("expected 3 violations, got %v", violations)
	}
}

func replaceSelectors(expression Expression, mutate func(*MetricSelector)) {
	switch node := expression.(type) {
	case *MetricSelector:
		mutate(node)
	case *RateExpression:
		mutate(node.Selector)
	case *Aggregation:
		replaceSelectors(node.Expr, mutate)
	case *HistogramQuantileExpression:
		replaceSelectors(node.Expr, mutate)
	case *BinaryExpression:
		replaceSelectors(node.Left, mutate)
		replaceSelectors(node.Right, mutate)
	}
}

func replaceRates(expression Expression, mutate func(*RateExpression)) {
	switch node := expression.(type) {
	case *RateExpression:
		mutate(node)
	case *Aggregation:
		replaceRates(node.Expr, mutate)
	case *HistogramQuantileExpression:
		replaceRates(node.Expr, mutate)
	case *BinaryExpression:
		replaceRates(node.Left, mutate)
		replaceRates(node.Right, mutate)
	}
}

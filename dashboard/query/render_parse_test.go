package query

import (
	"strings"
	"testing"
)

// TestRenderExact pins the exact rendered bytes for every supported node.
func TestRenderExact(t *testing.T) {
	cases := []struct {
		name string
		expr Expression
		want string
	}{
		{
			name: "selector",
			expr: &MetricSelector{MetricName: "http_requests_total", Matchers: []LabelMatcher{
				{Label: "operation", Op: MatchEqual, Value: "get"},
				{Label: "service", Op: MatchEqual, Value: "payment"},
			}},
			want: `http_requests_total{operation="get",service="payment"}`,
		},
		{
			name: "bucket selector",
			expr: &MetricSelector{MetricName: "duration_bucket", Matchers: []LabelMatcher{
				{Label: "le", Op: MatchNotEqual, Value: "+Inf"},
			}},
			want: `duration_bucket{le!="+Inf"}`,
		},
		{
			name: "error status regex",
			expr: &MetricSelector{MetricName: "requests_total", Matchers: []LabelMatcher{
				{Label: "status", Op: MatchRegex, Value: ErrorStatusPattern},
			}},
			want: `requests_total{status=~"5[0-9]{2}|error"}`,
		},
		{
			name: "rate",
			expr: &RateExpression{
				Selector: &MetricSelector{MetricName: "requests_total", Matchers: []LabelMatcher{
					{Label: "service", Op: MatchEqual, Value: "payment"},
				}},
				Interval: "$__rate_interval",
			},
			want: `rate(requests_total{service="payment"}[$__rate_interval])`,
		},
		{
			name: "sum by",
			expr: &Aggregation{By: []string{"operation"}, Expr: &MetricSelector{MetricName: "requests_total"}},
			want: `sum by (operation) (requests_total)`,
		},
		{
			name: "quantile",
			expr: &HistogramQuantileExpression{Quantile: 0.95, Expr: &MetricSelector{MetricName: "buckets"}},
			want: `histogram_quantile(0.95, buckets)`,
		},
		{
			name: "binary division",
			expr: &BinaryExpression{
				Op:    BinaryDivide,
				Left:  &ScalarExpression{Value: 1},
				Right: &ScalarExpression{Value: 2},
			},
			want: `(1 / 2)`,
		},
		{
			name: "scalar",
			expr: &ScalarExpression{Value: 0.5},
			want: `0.5`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := Render(test.expr)
			if err != nil {
				t.Fatalf("Render failed: %v", err)
			}
			if got != test.want {
				t.Errorf("Render = %q, want %q", got, test.want)
			}
		})
	}
}

// TestRenderRejectsUnsupportedNodes defends the closed node set.
func TestRenderRejectsUnsupportedNodes(t *testing.T) {
	if _, err := Render(nil); err == nil {
		t.Error("Render(nil) must fail")
	}
}

// TestRoundTrip is AC4 for every plan of the full item: render, parse,
// semantic equality, ten times with identical bytes.
func TestRoundTrip(t *testing.T) {
	item := fullItem()
	plans, links, diagnostics := PlanItemQueries(item, "payment", resolvePolicy(t))
	if len(diagnostics) != 0 {
		t.Fatalf("fixture produced diagnostics: %v", diagnostics)
	}
	expressions := make([]Expression, 0, len(plans))
	for _, plan := range plans {
		expressions = append(expressions, plan.Expression)
	}
	for round := range 10 {
		for _, expression := range expressions {
			first, err := Render(expression)
			if err != nil {
				t.Fatalf("render round %d: %v", round, err)
			}
			parsed, err := Parse(first)
			if err != nil {
				t.Fatalf("round %d: parse %q: %v", round, first, err)
			}
			second, err := Render(parsed)
			if err != nil {
				t.Fatalf("round %d: re-render: %v", round, err)
			}
			if first != second {
				t.Errorf("round %d: re-render differs: %q vs %q", round, first, second)
			}
			if !Equal(expression, parsed) {
				t.Errorf("round %d: parsed tree not semantically equal for %q", round, first)
			}
		}
	}
	_ = links
}

// TestParseRejects pins the closed-subset boundary: anything outside the
// supported grammar fails instead of degrading.
func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "bare selector", text: `{service="payment"}`},
		{name: "unsupported function", text: `absent(requests_total)`},
		{name: "unsupported aggregation", text: `avg(requests_total)`},
		{name: "comment", text: `requests_total # comment`},
		{name: "trailing junk", text: `requests_total{} garbage`},
		{name: "unterminated string", text: `requests_total{service="payment}`},
		{name: "unknown byte", text: `requests_total%5Bx%5D`},
		{name: "bare operator", text: `/ 2`},
		{name: "empty grouping", text: `sum by () (requests_total)`},
		{name: "unbalanced", text: `sum by (operation) (requests_total`},
		{name: "injection operator", text: `requests_total{service="payment"}` + ` + on(instance) group_left(version) other_metric`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if expression, err := Parse(test.text); err == nil {
				rendered, _ := Render(expression)
				t.Errorf("Parse accepted %q as %q", test.text, rendered)
			}
		})
	}
}

// TestParseRejectsInjectionTokens checks that escape sequences and
// unbalanced syntax cannot smuggle matcher structure.
func TestParseRejectsInjectionTokens(t *testing.T) {
	attempts := []string{
		`requests_total{service="x\",operation=\"y"}`,
		`requests_total{service="x\"}`,
		`rate(requests_total[$__rate_interval]} )`,
	}
	for _, attempt := range attempts {
		if _, err := Parse(attempt); err == nil {
			t.Errorf("Parse accepted injection attempt %q", attempt)
		}
	}
}

// TestEqual compares typed trees across node kinds.
func TestEqual(t *testing.T) {
	first := &Aggregation{By: []string{"operation"}, Expr: &MetricSelector{MetricName: "a"}}
	if !Equal(first, &Aggregation{By: []string{"operation"}, Expr: &MetricSelector{MetricName: "a"}}) {
		t.Error("identical trees must be equal")
	}
	if Equal(first, &Aggregation{By: []string{"operation"}, Expr: &MetricSelector{MetricName: "b"}}) {
		t.Error("different metric names must not be equal")
	}
	if Equal(first, &RateExpression{Selector: &MetricSelector{MetricName: "a"}, Interval: "$__rate_interval"}) {
		t.Error("different node kinds must not be equal")
	}
	if !Equal(nil, nil) {
		t.Error("nil trees must be equal")
	}
	if Equal(nil, first) {
		t.Error("nil and non-nil must not be equal")
	}
}

// TestParseIntervalDocuments the controlled interval token.
func TestParseInterval(t *testing.T) {
	text := `rate(requests_total[$__rate_interval])`
	parsed, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	rendered, err := Render(parsed)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if rendered != text {
		t.Errorf("round trip = %q, want %q", rendered, text)
	}
	if !strings.Contains(rendered, "$__rate_interval") {
		t.Errorf("controlled interval lost: %q", rendered)
	}
}

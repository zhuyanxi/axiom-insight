package query

import (
	"strings"
	"testing"
)

// FuzzParseNoPanic feeds arbitrary bytes to the subset parser. Parse must
// never panic, and a successfully parsed expression must re-render and
// re-parse to a semantically equal tree (no arbitrary token injection
// survives).
func FuzzParseNoPanic(f *testing.F) {
	for _, seed := range []string{
		`http_requests_total`,
		`sum by (operation) (rate(http_requests_total{service="payment",operation="get"}[$__rate_interval]))`,
		`(sum by (operation) (rate(http_requests_total{status=~"5[0-9]{2}|error"}[$__rate_interval])) / sum by (operation) (rate(http_requests_total[$__rate_interval])))`,
		`histogram_quantile(0.95, sum by (le) (rate(duration{le!="+Inf"}[$__rate_interval])))`,
		`sum by (operation) (http_in_flight{service="payment"})`,
		`0.5`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		expression, err := Parse(text)
		if err != nil {
			return
		}
		rendered, err := Render(expression)
		if err != nil {
			t.Fatalf("render of parsed %q failed: %v", text, err)
		}
		reparsed, err := Parse(rendered)
		if err != nil {
			t.Fatalf("reparse of %q failed: %v", rendered, err)
		}
		if !Equal(expression, reparsed) {
			t.Fatalf("round trip not stable for %q -> %q", text, rendered)
		}
	})
}

// FuzzValidateNoPanic feeds arbitrary expressions from the parser into
// the validator with a real item; validation must never panic and never
// echo expression content into messages.
func FuzzValidateNoPanic(f *testing.F) {
	item := fullItem()
	for _, seed := range []string{
		`sum by (operation) (rate(http_requests_total{service="payment"}[$__rate_interval]))`,
		`histogram_quantile(0.95, sum by (le) (rate(http_request_duration{le!="+Inf"}[$__rate_interval])))`,
		`http_requests_total`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		expression, err := Parse(text)
		if err != nil {
			return
		}
		plan := QueryPlan{
			CanonicalKey: "query:rate:item:http:get_user:rate",
			Kind:         QueryKindRate,
			ItemID:       item.ID,
			PlanIDs:      []string{"m_counter"},
			Expression:   expression,
		}
		for _, violation := range ValidatePlan(&plan, item, "payment") {
			if strings.Contains(violation.Message, text) {
				t.Fatalf("validator echoes expression content: %v", violation)
			}
		}
	})
}

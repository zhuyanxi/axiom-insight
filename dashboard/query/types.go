package query

// Typed query contract (P2-05). Renderers consume only these nodes; the
// PromQL text is a derived output, never an input. All node fields are
// controlled: metric names come from declared MetricPlans, matcher
// values come from normalized names or fixed allowlisted patterns.
const (
	// MaxBreakdownOperations caps the operation breakdown result count
	// (P2-05 task 6); exceeding it fails with DASHBOARD_PANEL_LIMIT_EXCEEDED.
	MaxBreakdownOperations = 32
	// ErrorStatusPattern is the only allowlisted regex value, matching
	// HTTP 5xx status codes and the literal error status. Raw user regexes
	// are never accepted.
	ErrorStatusPattern = `5[0-9]{2}|error`
	// rateIntervalLeMatcher is the fixed bucket-boundary exclusion.
	rateIntervalLeMatcher = "+Inf"
)

// FixedQuantiles is the fixed percentile order for latency panels
// (P2-05 task 4); the order never changes.
var FixedQuantiles = []float64{0.50, 0.95, 0.99}

// QueryKind classifies one generated query.
type QueryKind string

// Query kinds.
const (
	QueryKindRate       QueryKind = "rate"
	QueryKindErrorRatio QueryKind = "error_ratio"
	QueryKindPercentile QueryKind = "percentile"
	QueryKindInFlight   QueryKind = "in_flight"
	QueryKindBreakdown  QueryKind = "operation_breakdown"
	// QueryKindTopFailing is the overview error-rate-per-operation query
	// (P2-06): one family target for the Top Failing Operations table.
	QueryKindTopFailing QueryKind = "top_failing"
)

// Expression is one typed PromQL expression node. The marker method
// seals the node set: the renderer, parser and validator switch over the
// concrete types and reject anything else.
type Expression interface {
	exprNode()
}

// MetricSelector selects one exact declared metric with controlled label
// matchers. A selector without a metric name is invalid; bare selectors
// never appear as a plan root.
type MetricSelector struct {
	// MetricName is the exact declared MetricPlan name.
	MetricName string
	// Matchers are the controlled label matchers, sorted by label name.
	Matchers []LabelMatcher
}

// MatcherOp is one controlled matcher operator.
type MatcherOp int

// Matcher operators.
const (
	MatchEqual    MatcherOp = iota // label="value"
	MatchNotEqual                  // label!="value"
	MatchRegex                     // label=~"pattern"
)

func (*MetricSelector) exprNode() {}

// LabelMatcher is one label constraint. Label names come from the fixed
// vocabulary (service, operation, status, le); values are controlled
// normalized names or the fixed ErrorStatusPattern.
type LabelMatcher struct {
	// Label is the controlled label name.
	Label string
	// Op is the matcher operator.
	Op MatcherOp
	// Value is the controlled value or allowlisted pattern.
	Value string
}

// Aggregation wraps an expression in an aggregation. v1 supports only sum
// over the fixed label sets: operation (rate, breakdown) or le
// (percentile bucket sum).
type Aggregation struct {
	// By are the fixed group labels, sorted.
	By []string
	// Expr is the aggregated expression.
	Expr Expression
}

func (*Aggregation) exprNode() {}

// RateExpression applies rate() over a selector with the policy-fixed
// rate interval. Only counter selectors and histogram bucket selectors
// may be rated.
type RateExpression struct {
	// Selector is the rated selector.
	Selector *MetricSelector
	// Interval is the policy-fixed rate window ($__rate_interval).
	Interval string
}

func (*RateExpression) exprNode() {}

// HistogramQuantileExpression applies histogram_quantile over the bucket
// sum; the quantile is one of FixedQuantiles.
type HistogramQuantileExpression struct {
	// Quantile is the fixed quantile (0.50, 0.95 or 0.99).
	Quantile float64
	// Expr is the bucket aggregation.
	Expr Expression
}

func (*HistogramQuantileExpression) exprNode() {}

// BinaryOp is one controlled binary operator.
type BinaryOp int

// Binary operators.
const (
	BinaryDivide BinaryOp = iota // /
)

// BinaryExpression combines two expressions; v1 supports only division
// (error ratio numerator over denominator).
type BinaryExpression struct {
	// Op is the binary operator.
	Op BinaryOp
	// Left is the numerator expression.
	Left Expression
	// Right is the denominator expression.
	Right Expression
}

func (*BinaryExpression) exprNode() {}

// ScalarExpression is a numeric literal used as a ratio denominator when
// the metric is present but the status matcher admits no series.
type ScalarExpression struct {
	// Value is the literal.
	Value float64
}

func (*ScalarExpression) exprNode() {}

// QueryPlan is one fully traceable generated query. Renderers emit only
// Metadata-free PromQL; the typed Expression plus Metadata carry the
// traceability contract (AC1).
type QueryPlan struct {
	// CanonicalKey is the deterministic query identity used for refId
	// allocation: "query:<kind>:<itemID>:<purpose>".
	CanonicalKey string
	// Kind is the query class.
	Kind QueryKind
	// ItemID references the catalog item.
	ItemID string
	// Purpose is the fixed panel purpose, e.g. "rate" or "p95".
	Purpose string
	// PlanIDs are the sorted MetricPlan IDs this query is built from.
	PlanIDs []string
	// Expression is the typed expression tree.
	Expression Expression
	// Metadata carries provenance; it never reaches the rendered expr.
	Metadata QueryMetadata
}

// QueryMetadata is the internal traceability record (P2-05 task 9):
// canonical key, plan IDs, expression kind and safety provenance. It is
// never serialized into Grafana JSON.
type QueryMetadata struct {
	// Kind mirrors the plan kind.
	Kind QueryKind
	// CanonicalKey mirrors the plan canonical key.
	CanonicalKey string
	// PlanIDs are the source MetricPlan/SpanPlan IDs, sorted.
	PlanIDs []string
	// Provenance records the catalog input paths, e.g. "endpoints[2]".
	Provenance []string
	// RateInterval is the rate window used by rate() expressions.
	RateInterval string
	// Quantiles are the percentiles of a percentile plan, fixed order.
	Quantiles []float64
	// ErrorStatusPattern is the fixed error-status regex of an error
	// ratio numerator, empty otherwise.
	ErrorStatusPattern string
	// OperationValues are the controlled operation values of a breakdown
	// plan, empty otherwise.
	OperationValues []string
	// HashVersion pins the deterministic hashing contract (P2-04).
	HashVersion string
}

// TraceLinkPlan is one controlled Tempo deep-link model (P2-05 task 8).
// It carries only the fixed datasource variable, the validated service,
// operation and span names — never trace/request IDs, hosts or external
// URLs.
type TraceLinkPlan struct {
	// CanonicalKey is the deterministic link identity.
	CanonicalKey string
	// ItemID references the catalog item.
	ItemID string
	// PlanIDs are the source SpanPlan IDs, sorted.
	PlanIDs []string
	// DatasourceVariable is the policy-fixed datasource variable.
	DatasourceVariable string
	// ServiceName is the validated service name.
	ServiceName string
	// Operation is the validated item operation.
	Operation string
	// SpanName is the validated span name of the first supported span
	// plan (sorted by plan ID).
	SpanName string
	// Metadata carries provenance.
	Metadata QueryMetadata
}

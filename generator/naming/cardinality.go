package naming

import (
	"fmt"
	"math"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// SeriesEstimator computes classic-exposition series budgets from the
// finite attribute domains and the configured buckets or quantiles.
type SeriesEstimator struct{}

// EstimateSeries returns the classic-exposition series upper bound for
// one instrument:
//
//	counter/gauge: attributeCombinations
//	histogram:     attributeCombinations * (finiteBoundaries + 3)
//	               (finite buckets, implicit +Inf, sum, count)
//	summary:       attributeCombinations * (quantiles + 2)
//	               (quantiles, sum, count)
//
// The estimate is an upper bound; native histograms may emit fewer series
// at runtime but the planning budget must never be reduced.
func (SeriesEstimator) EstimateSeries(metricType string, attributeCardinalities []int, buckets, quantiles int) (int64, error) {
	combinations := int64(1)
	for _, cardinality := range attributeCardinalities {
		if cardinality < 1 {
			return 0, fmt.Errorf("attribute cardinality must be at least 1, got %d", cardinality)
		}
		if cardinality > math.MaxInt64/int(combinations) {
			return 0, fmt.Errorf("attribute cardinality product overflows int64")
		}
		combinations *= int64(cardinality)
	}
	switch metricType {
	case MetricTypeCounter, MetricTypeGauge:
		return combinations, nil
	case MetricTypeHistogram:
		if buckets < 1 {
			return 0, fmt.Errorf("histogram requires at least one bucket boundary")
		}
		if combinations > math.MaxInt64/int64(buckets+3) {
			return 0, fmt.Errorf("estimated series overflow int64")
		}
		return combinations * int64(buckets+3), nil
	case MetricTypeSummary:
		if quantiles < 1 {
			return 0, fmt.Errorf("summary requires at least one quantile")
		}
		if combinations > math.MaxInt64/int64(quantiles+2) {
			return 0, fmt.Errorf("estimated series overflow int64")
		}
		return combinations * int64(quantiles+2), nil
	default:
		return 0, fmt.Errorf("unsupported metric type %q", metricType)
	}
}

// BudgetCheck enforces the configured instrument and series limits. Any
// violation returns a GEN_CARDINALITY_LIMIT_EXCEEDED error carrying the
// estimate, the limit and the signal; plans are never silently truncated.
type BudgetCheck struct{}

// InstrumentBudget fails when the instrument count exceeds the limit.
func (BudgetCheck) InstrumentBudget(signal string, instruments, limit int64) error {
	if instruments <= limit {
		return nil
	}
	return &BudgetError{
		Signal:     signal,
		Estimates:  fmt.Sprintf("%d instruments", instruments),
		Limit:      limit,
	}
}

// SeriesBudget fails when the estimated series exceed the limit.
func (BudgetCheck) SeriesBudget(signal string, estimated, limit int64) error {
	if estimated <= limit {
		return nil
	}
	return &BudgetError{
		Signal:    signal,
		Estimates: fmt.Sprintf("%d estimated series", estimated),
		Limit:     limit,
	}
}

// BudgetError reports a cardinality budget violation.
type BudgetError struct {
	// Signal is the affected signal name.
	Signal string
	// Estimates describes the computed upper bound, e.g. "120 estimated series".
	Estimates string
	// Limit is the configured policy limit.
	Limit int64
}

// Error implements error with the stable GEN_CARDINALITY_LIMIT_EXCEEDED
// code and no plan content.
func (failure *BudgetError) Error() string {
	return fmt.Sprintf("%s: %s: %s exceeds the configured limit of %d; refusing to truncate",
		policy.CodeCardinalityLimitExceeded, failure.Signal, failure.Estimates, failure.Limit)
}

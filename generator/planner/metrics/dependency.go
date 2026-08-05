package metrics

import (
	"context"
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// DependencyMetricsPlanner plans metric instruments for the six Phase 0
// dependency kinds (Kafka producer/consumer, SQL, Redis, HTTP client,
// RPC client). It is stateless and safe for concurrent use.
type DependencyMetricsPlanner struct{}

// dependencySpec carries the per-kind constants of the default
// dependency metric mapping (P1-07 table).
type dependencySpec struct {
	// system is the controlled dependency system name.
	system string
	// displayName is used in descriptions.
	displayName string
	// counterUnit is the operations counter unit.
	counterUnit string
	// gaugeUnit is the in-flight gauge unit.
	gaugeUnit string
}

// dependencyKinds is the exhaustive DependencyKind mapping. The system is
// derived exclusively from the kind; third-party systems are never
// guessed from method names or target values. A kind absent from this
// table is unsupported and must be skipped with GEN_UNSUPPORTED_ENTITY.
var dependencyKinds = map[observabilityv1.DependencyKind]dependencySpec{
	observabilityv1.DependencyKind_KAFKA_PRODUCER: {
		system: "kafka", displayName: "Kafka producer operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
	observabilityv1.DependencyKind_KAFKA_CONSUMER: {
		system: "kafka", displayName: "Kafka consumer operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
	observabilityv1.DependencyKind_SQL: {
		system: "sql", displayName: "SQL operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
	observabilityv1.DependencyKind_REDIS: {
		system: "redis", displayName: "Redis operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
	observabilityv1.DependencyKind_HTTP_CLIENT: {
		system: "http", displayName: "HTTP client operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
	observabilityv1.DependencyKind_RPC_CLIENT: {
		system: "rpc", displayName: "RPC client operations", counterUnit: "{operation}", gaugeUnit: "{operation}",
	},
}

// dependencyPurpose builds the purpose suffix for a system, e.g.
// "sql_operations_total".
func dependencyPurpose(system, suffix string) string {
	return system + "_" + suffix
}

// PlanMetrics implements planner.MetricsPlanner for dependencies. Per
// dependency it emits an operations counter and a duration histogram by
// default, plus an in-flight gauge and a duration summary when the policy
// enables them. Raw target values (URLs, SQL text, keys, payloads,
// resources) never enter names, descriptions, attributes or diagnostics;
// dynamic targets degrade to generic metrics with GEN_INCOMPLETE_TARGET.
func (DependencyMetricsPlanner) PlanMetrics(ctx context.Context, input *planner.SignalInput) (*planner.MetricsResult, error) {
	result := &planner.MetricsResult{}
	estimator := naming.SeriesEstimator{}
	var instruments, series int64

	serviceName := input.Document.GetService().GetName()
	for _, dependency := range input.Document.Dependencies {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec, supported := dependencyKinds[dependency.GetKind()]
		if !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalMetrics,
				TargetID: dependency.GetId(), Field: "kind",
				Message: "dependency kind is not supported by the dependency metrics mapping",
			})
			result.Skipped++
			continue
		}

		operation, diagnostics := dependencyOperation(dependency)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		if !dependency.GetValueIsStatic() {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeIncompleteTarget, Signal: planner.SignalMetrics,
				TargetID: dependency.GetId(), Field: "target",
				Message: "dependency target is dynamic; target values are omitted",
			})
		}

		prefix := naming.MetricNameSpec{
			Namespace: input.Policy.Metrics.Namespace,
			Service:   serviceName,
			Module:    modulePathFor(input.Index, dependency),
			Function:  functionNameFor(input.Index, dependency),
			Operation: operation,
		}
		metricName := func(purpose string) string {
			prefix.Purpose = purpose
			name, _, err := naming.NamingPolicy{}.MetricName(prefix)
			if err != nil {
				return purpose
			}
			return name
		}

		// Operations counter: increments at operation end with constant
		// value 1; status attribute expresses success/failure.
		result.Items = append(result.Items, &observabilityv1.MetricPlan{
			Id:          planner.StableID(planner.SignalMetrics, dependency.GetId(), planner.PurposeCount),
			Name:        metricName(dependencyPurpose(spec.system, "operations_total")),
			Type:        observabilityv1.MetricType_METRIC_TYPE_COUNTER,
			Unit:        spec.counterUnit,
			Description: fmt.Sprintf("Number of %s completed; unit %s; recorded at operation end", spec.displayName, spec.counterUnit),
			Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: dependency.GetId()},
			FunctionId:  dependency.GetFunctionId(),
			Trigger:     &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
			Value:       constantBinding("plan.constant.one", observabilityv1.ValueType_VALUE_TYPE_INT64),
			Attributes: []*observabilityv1.AttributeBinding{
				serviceAttribute(),
				operationAttribute(operation),
				statusAttribute(),
			},
		})
		instruments++
		series += counterSeries(&estimator)

		// Duration histogram in seconds with the policy buckets.
		result.Items = append(result.Items, &observabilityv1.MetricPlan{
			Id:          planner.StableID(planner.SignalMetrics, dependency.GetId(), planner.PurposeDuration),
			Name:        metricName(dependencyPurpose(spec.system, "operation_duration_seconds")),
			Type:        observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM,
			Unit:        "s",
			Description: fmt.Sprintf("Duration of %s; unit s; recorded at operation end", spec.displayName),
			Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: dependency.GetId()},
			FunctionId:  dependency.GetFunctionId(),
			Trigger:     &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
			Value:       durationBinding(),
			Attributes: []*observabilityv1.AttributeBinding{
				serviceAttribute(),
				operationAttribute(operation),
				statusAttribute(),
			},
		})
		instruments++
		series += histogramSeries(&estimator, input.Policy.Metrics.HistogramBucketsSeconds)

		if input.Policy.Metrics.IncludeInFlightGauges {
			// In-flight gauge: counts in-call operations only; never pool
			// depth, queue length or consumer lag.
			result.Items = append(result.Items, &observabilityv1.MetricPlan{
				Id:          planner.StableID(planner.SignalMetrics, dependency.GetId(), planner.PurposeInFlight),
				Name:        metricName(dependencyPurpose(spec.system, "operations_in_flight")),
				Type:        observabilityv1.MetricType_METRIC_TYPE_GAUGE,
				Unit:        spec.gaugeUnit,
				Description: fmt.Sprintf("Number of %s in flight (in-call operations only); unit %s; recorded on state change", spec.displayName, spec.gaugeUnit),
				Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: dependency.GetId()},
				FunctionId:  dependency.GetFunctionId(),
				Trigger:     &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_STATE_CHANGE},
				Value: &observabilityv1.ValueBinding{
					Path: "runtime.operation.in_flight", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
					Type: observabilityv1.ValueType_VALUE_TYPE_INT64, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
				},
				Attributes: []*observabilityv1.AttributeBinding{
					serviceAttribute(),
					operationAttribute(operation),
				},
			})
			instruments++
			series += gaugeSeries(&estimator)
		}

		if input.Policy.Metrics.Summaries.Enabled {
			result.Items = append(result.Items, &observabilityv1.MetricPlan{
				Id:          planner.StableID(planner.SignalMetrics, dependency.GetId(), planner.PurposeDurationSummary),
				Name:        metricName(dependencyPurpose(spec.system, "operation_duration_seconds_summary")),
				Type:        observabilityv1.MetricType_METRIC_TYPE_SUMMARY,
				Unit:        "s",
				Description: fmt.Sprintf("Duration summary of %s; unit s; recorded at operation end", spec.displayName),
				Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_DEPENDENCY, Id: dependency.GetId()},
				FunctionId:  dependency.GetFunctionId(),
				Trigger:     &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
				Value:       durationBinding(),
				Attributes: []*observabilityv1.AttributeBinding{
					serviceAttribute(),
					operationAttribute(operation),
					statusAttribute(),
				},
			})
			instruments++
			series += summarySeries(&estimator, input.Policy.Metrics.Summaries.Quantiles)
		}
	}

	// Name collisions within the dependency metrics (e.g. two call sites
	// with the same operation) are disambiguated by the unified P1-04
	// policy; the mapping is deterministic and order-independent.
	disambiguateItems(result)

	check := naming.BudgetCheck{}
	if err := check.InstrumentBudget(planner.SignalMetrics, instruments, input.Policy.Metrics.MaxInstruments); err != nil {
		return nil, err
	}
	if err := check.SeriesBudget(planner.SignalMetrics, series, input.Policy.Metrics.MaxEstimatedSeries); err != nil {
		return nil, err
	}

	sort.Slice(result.Items, func(left, right int) bool {
		return result.Items[left].GetId() < result.Items[right].GetId()
	})
	for _, item := range result.Items {
		sort.Slice(item.Attributes, func(left, right int) bool {
			return item.Attributes[left].GetKey() < item.Attributes[right].GetKey()
		})
	}
	return result, nil
}

// dependencyOperation normalizes the dependency operation; a missing
// operation degrades to the controlled "unknown" with
// GEN_INCOMPLETE_TARGET. Raw target values never enter the operation.
func dependencyOperation(dependency *observabilityv1.Dependency) (string, []naming.Diagnostic) {
	if dependency.GetOperation() == "" {
		return "unknown", []naming.Diagnostic{{
			Code: policy.CodeIncompleteTarget, Signal: planner.SignalMetrics,
			TargetID: dependency.GetId(), Field: "operation",
			Message: "dependency operation is missing; using the controlled fallback operation",
		}}
	}
	operation, err := naming.NormalizeMachineName(dependency.GetOperation())
	if err != nil {
		return "unknown", []naming.Diagnostic{{
			Code: policy.CodeIncompleteTarget, Signal: planner.SignalMetrics,
			TargetID: dependency.GetId(), Field: "operation",
			Message: "dependency operation is not usable; using the controlled fallback operation",
		}}
	}
	return operation, nil
}

func modulePathFor(index *planner.Index, dependency *observabilityv1.Dependency) string {
	function := index.Function(dependency.GetFunctionId())
	if function == nil {
		return ""
	}
	return function.GetPackagePath()
}

func functionNameFor(index *planner.Index, dependency *observabilityv1.Dependency) string {
	function := index.Function(dependency.GetFunctionId())
	if function == nil {
		return ""
	}
	return function.GetQualifiedName()
}

// disambiguateItems applies the unified P1-04 collision table to the
// dependency metric names and rewrites suffixed names onto the items.
// Items are matched back by (target ID, original name): several items of
// one dependency share the target ID (counter, histogram, gauge), so a
// target-ID-only map would collapse them onto one name.
func disambiguateItems(result *planner.MetricsResult) {
	if len(result.Items) < 2 {
		return
	}
	originals := make([]string, len(result.Items))
	nameItems := make([]naming.NameItem, len(result.Items))
	for index, item := range result.Items {
		originals[index] = item.GetName()
		nameItems[index] = naming.NameItem{
			Signal: planner.SignalMetrics, TargetID: item.GetTarget().GetId(), Name: originals[index],
		}
	}
	resolved, diagnostics := naming.NamingPolicy{}.Disambiguate(nameItems)
	for index, item := range result.Items {
		for _, entry := range resolved {
			if entry.TargetID != item.GetTarget().GetId() {
				continue
			}
			if entry.Name == originals[index] ||
				entry.Name == originals[index]+"_"+naming.DisambiguationSuffix(entry.TargetID) {
				item.Name = entry.Name
				break
			}
		}
	}
	result.Diagnostics = append(result.Diagnostics, diagnostics.Items()...)
}

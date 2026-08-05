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

// EndpointMetricsPlanner plans metric instruments for HTTP, gRPC and Cron
// endpoints. It is stateless and safe for concurrent use.
type EndpointMetricsPlanner struct{}

// endpointSpec carries the per-kind constants of the default endpoint
// metric mapping (P1-06 table).
type endpointSpec struct {
	// kindName is used in descriptions, e.g. "HTTP requests".
	kindName string
	// purpose suffixes per instrument type.
	purposeCount   string
	purposeDuration string
	purposeSummary string
	purposeInFlight string
	// counterUnit is the counter unit, e.g. "{request}".
	counterUnit string
	// gaugeUnit is the in-flight gauge unit, always "{operation}".
	gaugeUnit string
}

// endpointKinds is the exhaustive EndpointKind mapping. A kind absent
// from this table is unsupported and must be skipped with
// GEN_UNSUPPORTED_ENTITY — never guessed.
var endpointKinds = map[observabilityv1.EndpointKind]endpointSpec{
	observabilityv1.EndpointKind_HTTP_HANDLER: {
		kindName: "HTTP requests", counterUnit: "{request}", gaugeUnit: "{operation}",
		purposeCount: "http_requests_total", purposeDuration: "http_request_duration_seconds",
		purposeSummary: "http_request_duration_seconds_summary", purposeInFlight: "http_requests_in_flight",
	},
	observabilityv1.EndpointKind_GRPC_HANDLER: {
		kindName: "gRPC requests", counterUnit: "{request}", gaugeUnit: "{operation}",
		purposeCount: "grpc_requests_total", purposeDuration: "grpc_request_duration_seconds",
		purposeSummary: "grpc_request_duration_seconds_summary", purposeInFlight: "grpc_requests_in_flight",
	},
	observabilityv1.EndpointKind_CRON_JOB: {
		kindName: "cron runs", counterUnit: "{run}", gaugeUnit: "{operation}",
		purposeCount: "cron_runs_total", purposeDuration: "cron_run_duration_seconds",
		purposeSummary: "cron_run_duration_seconds_summary", purposeInFlight: "cron_jobs_in_flight",
	},
}

// PlanMetrics implements planner.MetricsPlanner for endpoints. It emits,
// per endpoint: a completion counter, a duration histogram, an in-flight
// gauge when the policy enables it and a duration summary when the policy
// enables it. The whole signal fails when the estimated instruments or
// series exceed the policy limits.
func (EndpointMetricsPlanner) PlanMetrics(ctx context.Context, input *planner.SignalInput) (*planner.MetricsResult, error) {
	result := &planner.MetricsResult{}
	estimator := naming.SeriesEstimator{}
	var instruments, series int64

	serviceName := input.Document.GetService().GetName()
	for _, endpoint := range input.Document.Endpoints {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		spec, supported := endpointKinds[endpoint.GetKind()]
		if !supported {
			result.Diagnostics = append(result.Diagnostics, naming.Diagnostic{
				Code: policy.CodeUnsupportedEntity, Signal: planner.SignalMetrics,
				TargetID: endpoint.GetId(), Field: "kind",
				Message: "endpoint kind is not supported by the endpoint metrics mapping",
			})
			result.Skipped++
			continue
		}

		operation, _, diagnostics := endpointOperation(endpoint, input.Index)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)

		prefix := naming.MetricNameSpec{
			Namespace: input.Policy.Metrics.Namespace,
			Service:   serviceName,
			Module:    modulePath(input.Index, endpoint),
			Function:  functionName(input.Index, endpoint),
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

		// Counter: increments at operation end with constant value 1.
		counterID := planner.StableID(planner.SignalMetrics, endpoint.GetId(), planner.PurposeCount)
		result.Items = append(result.Items, &observabilityv1.MetricPlan{
			Id:          counterID,
			Name:        metricName(spec.purposeCount),
			Type:        observabilityv1.MetricType_METRIC_TYPE_COUNTER,
			Unit:        spec.counterUnit,
			Description: fmt.Sprintf("Number of %s completed; unit %s; recorded at operation end", spec.kindName, spec.counterUnit),
			Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId()},
			FunctionId:  endpoint.GetFunctionId(),
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

		// Histogram: duration in seconds with the policy buckets.
		histogramID := planner.StableID(planner.SignalMetrics, endpoint.GetId(), planner.PurposeDuration)
		result.Items = append(result.Items, &observabilityv1.MetricPlan{
			Id:          histogramID,
			Name:        metricName(spec.purposeDuration),
			Type:        observabilityv1.MetricType_METRIC_TYPE_HISTOGRAM,
			Unit:        "s",
			Description: fmt.Sprintf("Duration of %s; unit s; recorded at operation end", spec.kindName),
			Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId()},
			FunctionId:  endpoint.GetFunctionId(),
			Trigger:     &observabilityv1.Trigger{Phase: observabilityv1.TriggerPhase_TRIGGER_PHASE_END},
			Value:       durationBinding(),
			Attributes: []*observabilityv1.AttributeBinding{
				serviceAttribute(),
				operationAttribute(operation),
				statusAttribute(),
			},
		})
		// Bucket boundaries are not part of the plan contract; they flow
		// from the policy into the renderer stage (P1-08) and into the
		// series estimate here.
		instruments++
		series += histogramSeries(&estimator, input.Policy.Metrics.HistogramBucketsSeconds)

		if input.Policy.Metrics.IncludeInFlightGauges {
			gaugeID := planner.StableID(planner.SignalMetrics, endpoint.GetId(), planner.PurposeInFlight)
			result.Items = append(result.Items, &observabilityv1.MetricPlan{
				Id:          gaugeID,
				Name:        metricName(spec.purposeInFlight),
				Type:        observabilityv1.MetricType_METRIC_TYPE_GAUGE,
				Unit:        spec.gaugeUnit,
				Description: fmt.Sprintf("Number of %s in flight; unit %s; recorded on state change", spec.kindName, spec.gaugeUnit),
				Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId()},
				FunctionId:  endpoint.GetFunctionId(),
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
			summaryID := planner.StableID(planner.SignalMetrics, endpoint.GetId(), planner.PurposeDurationSummary)
			result.Items = append(result.Items, &observabilityv1.MetricPlan{
				Id:          summaryID,
				Name:        metricName(spec.purposeSummary),
				Type:        observabilityv1.MetricType_METRIC_TYPE_SUMMARY,
				Unit:        "s",
				Description: fmt.Sprintf("Duration summary of %s; unit s; recorded at operation end", spec.kindName),
				Target:      &observabilityv1.TargetRef{Kind: observabilityv1.TargetKind_TARGET_KIND_ENDPOINT, Id: endpoint.GetId()},
				FunctionId:  endpoint.GetFunctionId(),
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

	// Budget checks: any overflow fails the whole signal, never a partial
	// endpoint metrics result.
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

// endpointOperation derives the normalized operation per endpoint kind.
// HTTP uses the static method (fallback "http" on missing method or
// route), gRPC uses service/method (fallback to the function identity),
// cron uses the stable job name. Degradations report
// GEN_INCOMPLETE_TARGET without raw values.
func endpointOperation(endpoint *observabilityv1.Endpoint, index *planner.Index) (string, bool, []naming.Diagnostic) {
	var diagnostics []naming.Diagnostic
	emit := func(operation string, message string) (string, bool, []naming.Diagnostic) {
		diagnostics = append(diagnostics, naming.Diagnostic{
			Code: policy.CodeIncompleteTarget, Signal: planner.SignalMetrics,
			TargetID: endpoint.GetId(), Field: "operation",
			Message: message,
		})
		return operation, true, diagnostics
	}

	switch endpoint.GetKind() {
	case observabilityv1.EndpointKind_HTTP_HANDLER:
		method := endpoint.GetHttpMethod()
		if method == "" || endpoint.GetHttpPath() == "" {
			return emit("http", "endpoint method or route is missing; using a controlled fallback operation")
		}
		operation, err := naming.NormalizeMachineName(method)
		if err != nil {
			return emit("http", "endpoint method is not usable; using a controlled fallback operation")
		}
		return operation, false, nil
	case observabilityv1.EndpointKind_GRPC_HANDLER:
		if endpoint.GetGrpcService() != "" && endpoint.GetGrpcMethod() != "" {
			operation, err := naming.NormalizeMachineName(endpoint.GetGrpcService() + "/" + endpoint.GetGrpcMethod())
			if err == nil {
				return operation, false, nil
			}
		}
		function := index.Function(endpoint.GetFunctionId())
		if function != nil && function.GetQualifiedName() != "" {
			operation, err := naming.NormalizeMachineName(function.GetQualifiedName())
			if err == nil {
				return operation, true, []naming.Diagnostic{{
					Code: policy.CodeIncompleteTarget, Signal: planner.SignalMetrics,
					TargetID: endpoint.GetId(), Field: "operation",
					Message: "gRPC service or method is missing; falling back to the function identity",
				}}
			}
		}
		return emit("grpc", "gRPC service, method and function identity are missing; using a controlled fallback operation")
	case observabilityv1.EndpointKind_CRON_JOB:
		if endpoint.GetName() != "" {
			operation, err := naming.NormalizeMachineName(endpoint.GetName())
			if err == nil {
				return operation, false, nil
			}
		}
		return emit("cron", "cron job name is missing; using a controlled fallback operation")
	default:
		return "unknown", true, nil
	}
}

// modulePath resolves the owning function's package path for the metric
// name; empty when absent.
func modulePath(index *planner.Index, endpoint *observabilityv1.Endpoint) string {
	function := index.Function(endpoint.GetFunctionId())
	if function == nil {
		return ""
	}
	return function.GetPackagePath()
}

// functionName resolves the owning function's qualified name; empty when
// absent.
func functionName(index *planner.Index, endpoint *observabilityv1.Endpoint) string {
	function := index.Function(endpoint.GetFunctionId())
	if function == nil {
		return ""
	}
	return function.GetQualifiedName()
}

func serviceAttribute() *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: "service",
		Value: &observabilityv1.ValueBinding{
			Path: "service.name", Source: observabilityv1.ValueSource_VALUE_SOURCE_IR_CONSTANT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
		},
	}
}

func operationAttribute(operation string) *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: "operation",
		Value: constantBinding("plan.constant."+operation, observabilityv1.ValueType_VALUE_TYPE_STRING),
	}
}

func statusAttribute() *observabilityv1.AttributeBinding {
	return &observabilityv1.AttributeBinding{
		Key: "status",
		Value: &observabilityv1.ValueBinding{
			Path: "runtime.operation.status", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
			Type: observabilityv1.ValueType_VALUE_TYPE_STATUS, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
		},
	}
}

func constantBinding(path string, valueType observabilityv1.ValueType) *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: path, Source: observabilityv1.ValueSource_VALUE_SOURCE_PLAN_CONSTANT,
		Type: valueType, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_CONSTANT,
	}
}

func durationBinding() *observabilityv1.ValueBinding {
	return &observabilityv1.ValueBinding{
		Path: "runtime.operation.duration_seconds", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
		Type: observabilityv1.ValueType_VALUE_TYPE_DOUBLE, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
	}
}

// Series estimates: service and operation are constant per target; status
// has exactly five values and never applies to gauges.
func counterSeries(estimator *naming.SeriesEstimator) int64 {
	series, _ := estimator.EstimateSeries(naming.MetricTypeCounter, []int{1, 1, 5}, 0, 0)
	return series
}

func histogramSeries(estimator *naming.SeriesEstimator, buckets []float64) int64 {
	series, _ := estimator.EstimateSeries(naming.MetricTypeHistogram, []int{1, 1, 5}, len(buckets), 0)
	return series
}

func gaugeSeries(estimator *naming.SeriesEstimator) int64 {
	series, _ := estimator.EstimateSeries(naming.MetricTypeGauge, []int{1, 1}, 0, 0)
	return series
}

func summarySeries(estimator *naming.SeriesEstimator, quantiles []float64) int64 {
	series, _ := estimator.EstimateSeries(naming.MetricTypeSummary, []int{1, 1, 5}, 0, len(quantiles))
	return series
}

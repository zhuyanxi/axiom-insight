package tracing

import (
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// attachEvents adds the controlled error events to a span plan: a
// timeout event (fires on runtime timeout), a cancelled event (fires on
// runtime cancelled) and, when the policy enables it, an exception event
// (fires on runtime error) binding only the safe error type. Raw error
// strings and stacktraces are never bound. The event count per span is
// fixed at three at most.
func attachEvents(span *observabilityv1.SpanPlan, targetID string, resolved policy.Policy) {
	span.Events = append(span.Events,
		controlledEvent(targetID, planner.PurposeTimeout, "timeout", observabilityv1.RuntimeStatus_RUNTIME_STATUS_TIMEOUT, nil),
		controlledEvent(targetID, planner.PurposeCancelled, "cancelled", observabilityv1.RuntimeStatus_RUNTIME_STATUS_CANCELLED, nil),
	)
	if resolved.Tracing.RecordExceptionEvents {
		span.Events = append(span.Events, controlledEvent(
			targetID, planner.PurposeException, "exception", observabilityv1.RuntimeStatus_RUNTIME_STATUS_ERROR,
			[]*observabilityv1.AttributeBinding{{
				Key: "exception.type",
				Value: &observabilityv1.ValueBinding{
					Path: "runtime.error.type", Source: observabilityv1.ValueSource_VALUE_SOURCE_RUNTIME_RESULT,
					Type: observabilityv1.ValueType_VALUE_TYPE_STRING, Cardinality: observabilityv1.CardinalityClass_CARDINALITY_CLASS_LOW,
				},
			}},
		))
	}
}

// controlledEvent builds one event with a stable ID derived from the
// target and purpose; the event name is a controlled constant, never a
// raw error string.
func controlledEvent(targetID, purpose, name string, status observabilityv1.RuntimeStatus, attributes []*observabilityv1.AttributeBinding) *observabilityv1.SpanEvent {
	return &observabilityv1.SpanEvent{
		Id:         planner.StableID(planner.SignalTracing, targetID, purpose),
		Name:       name,
		Statuses:   []observabilityv1.RuntimeStatus{status},
		Attributes: attributes,
	}
}

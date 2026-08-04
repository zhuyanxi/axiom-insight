package planner

import (
	"sort"

	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// sortPlanItems sorts every orderable part of the plan with stable
// comparators: items by plan ID, attributes and fields by key, events by
// event ID, diagnostics by code then message. Sorting only ever touches
// the freshly built plan, never the input document.
func sortPlanItems(plan *observabilityv1.GenerationPlan) {
	sort.Slice(plan.Metrics, func(left, right int) bool {
		return plan.Metrics[left].GetId() < plan.Metrics[right].GetId()
	})
	for _, metric := range plan.Metrics {
		sort.Slice(metric.Attributes, func(left, right int) bool {
			return metric.Attributes[left].GetKey() < metric.Attributes[right].GetKey()
		})
	}

	sort.Slice(plan.Spans, func(left, right int) bool {
		return plan.Spans[left].GetId() < plan.Spans[right].GetId()
	})
	for _, span := range plan.Spans {
		sort.Slice(span.Attributes, func(left, right int) bool {
			return span.Attributes[left].GetKey() < span.Attributes[right].GetKey()
		})
		sort.Slice(span.Events, func(left, right int) bool {
			return span.Events[left].GetId() < span.Events[right].GetId()
		})
		for _, event := range span.Events {
			sort.Slice(event.Attributes, func(left, right int) bool {
				return event.Attributes[left].GetKey() < event.Attributes[right].GetKey()
			})
		}
	}

	sort.Slice(plan.Logs, func(left, right int) bool {
		return plan.Logs[left].GetId() < plan.Logs[right].GetId()
	})
	for _, logPlan := range plan.Logs {
		sort.Slice(logPlan.Fields, func(left, right int) bool {
			return logPlan.Fields[left].GetKey() < logPlan.Fields[right].GetKey()
		})
	}

	sort.Slice(plan.Diagnostics, func(left, right int) bool {
		if plan.Diagnostics[left].GetCode() != plan.Diagnostics[right].GetCode() {
			return plan.Diagnostics[left].GetCode() < plan.Diagnostics[right].GetCode()
		}
		return plan.Diagnostics[left].GetMessage() < plan.Diagnostics[right].GetMessage()
	})
}

package observabilityv1

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
)

func clonePlan(t *testing.T, plan *GenerationPlan) *GenerationPlan {
	t.Helper()
	clone := proto.Clone(plan)
	planClone, ok := clone.(*GenerationPlan)
	if !ok {
		t.Fatalf("clone plan: unexpected type %T", clone)
	}
	return planClone
}

func TestValidateGenerationPlanValid(t *testing.T) {
	violations := ValidateGenerationPlan(fullDocument(), fullGenerationPlan())
	if len(violations) != 0 {
		t.Fatalf("valid plan reported violations:\n%s", formatViolations(violations))
	}
}

func TestValidateGenerationPlanNilInputs(t *testing.T) {
	if violations := ValidateGenerationPlan(fullDocument(), nil); len(violations) != 1 || violations[0].Field != "generation_plan" {
		t.Fatalf("nil plan: got %v", violations)
	}
	if violations := ValidateGenerationPlan(nil, fullGenerationPlan()); len(violations) != 1 || violations[0].Field != "generation_plan" {
		t.Fatalf("nil document: got %v", violations)
	}
}

func TestValidateGenerationPlanEmptyPlan(t *testing.T) {
	violations := ValidateGenerationPlan(fullDocument(), &GenerationPlan{})
	fields := violationFields(violations)
	for _, want := range []string{"schema_version", "service_name"} {
		if !containsField(fields, want) {
			t.Errorf("empty plan missing violation for %q: got %v", want, fields)
		}
	}
}

type validateCase struct {
	name       string
	mutate     func(plan *GenerationPlan)
	wantField  string
	wantSubstr string
}

func TestValidateGenerationPlanViolations(t *testing.T) {
	cases := []validateCase{
		{
			name: "duplicate metric ID",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[1].Id = plan.Metrics[0].Id
			},
			wantField:  "metrics[1].id",
			wantSubstr: "duplicate",
		},
		{
			name: "duplicate ID across signal lists",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Id = plan.Metrics[0].Id
			},
			wantField:  "spans[0].id",
			wantSubstr: "duplicate",
		},
		{
			name: "empty metric ID",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Id = ""
			},
			wantField:  "metrics[0].id",
			wantSubstr: "empty",
		},
		{
			name: "empty metric name",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Name = ""
			},
			wantField:  "metrics[0].name",
			wantSubstr: "empty",
		},
		{
			name: "unspecified metric type",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Type = MetricType_METRIC_TYPE_UNSPECIFIED
			},
			wantField:  "metrics[0].type",
			wantSubstr: "unspecified",
		},
		{
			name: "missing metric target",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Target = nil
			},
			wantField:  "metrics[0].target",
			wantSubstr: "missing",
		},
		{
			name: "empty target ID",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Target.Id = ""
			},
			wantField:  "metrics[0].target.id",
			wantSubstr: "empty",
		},
		{
			name: "unspecified target kind",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Target.Kind = TargetKind_TARGET_KIND_UNSPECIFIED
			},
			wantField:  "metrics[0].target.kind",
			wantSubstr: "unspecified",
		},
		{
			name: "dangling target ID",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Target.Id = "endpoint:does-not-exist"
			},
			wantField:  "metrics[0].target.id",
			wantSubstr: "does not exist",
		},
		{
			name: "target kind mismatch",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Target = &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "endpoint:http:create-order"}
			},
			wantField:  "metrics[0].target.kind",
			wantSubstr: "does not match",
		},
		{
			name: "missing record trigger",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Trigger = nil
			},
			wantField:  "metrics[0].trigger",
			wantSubstr: "missing",
		},
		{
			name: "unspecified trigger phase",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Trigger.Phase = TriggerPhase_TRIGGER_PHASE_UNSPECIFIED
			},
			wantField:  "metrics[0].trigger.phase",
			wantSubstr: "unspecified",
		},
		{
			name: "missing metric value binding",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Value = nil
			},
			wantField:  "metrics[0].value",
			wantSubstr: "missing",
		},
		{
			name: "unspecified value source",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Value.Source = ValueSource_VALUE_SOURCE_UNSPECIFIED
			},
			wantField:  "metrics[0].value.source",
			wantSubstr: "unspecified",
		},
		{
			name: "unspecified value type",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Value.Type = ValueType_VALUE_TYPE_UNSPECIFIED
			},
			wantField:  "metrics[0].value.type",
			wantSubstr: "unspecified",
		},
		{
			name: "unspecified cardinality class",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Value.Cardinality = CardinalityClass_CARDINALITY_CLASS_UNSPECIFIED
			},
			wantField:  "metrics[0].value.cardinality",
			wantSubstr: "unspecified",
		},
		{
			name: "empty value path",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Value.Path = ""
			},
			wantField:  "metrics[0].value.path",
			wantSubstr: "empty",
		},
		{
			name: "empty attribute key",
			mutate: func(plan *GenerationPlan) {
				plan.Metrics[0].Attributes[0].Key = ""
			},
			wantField:  "metrics[0].attributes[0].key",
			wantSubstr: "empty",
		},
		{
			name: "unspecified span kind",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Kind = SpanKind_SPAN_KIND_UNSPECIFIED
			},
			wantField:  "spans[0].kind",
			wantSubstr: "unspecified",
		},
		{
			name: "empty span name",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Name = ""
			},
			wantField:  "spans[0].name",
			wantSubstr: "empty",
		},
		{
			name: "missing start trigger",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].StartTrigger = nil
			},
			wantField:  "spans[0].start_trigger",
			wantSubstr: "missing",
		},
		{
			name: "missing end trigger",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].EndTrigger = nil
			},
			wantField:  "spans[0].end_trigger",
			wantSubstr: "missing",
		},
		{
			name: "missing parent strategy",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Parent = nil
			},
			wantField:  "spans[0].parent",
			wantSubstr: "missing",
		},
		{
			name: "unspecified parent strategy mode",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Parent.Mode = ParentStrategyMode_PARENT_STRATEGY_MODE_UNSPECIFIED
			},
			wantField:  "spans[0].parent.mode",
			wantSubstr: "unspecified",
		},
		{
			name: "static parent mode without span ID",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Parent = &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_STATIC}
			},
			wantField:  "spans[0].parent.static_parent_span_id",
			wantSubstr: "requires",
		},
		{
			name: "static parent span ID outside static mode",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Parent = &ParentStrategy{Mode: ParentStrategyMode_PARENT_STRATEGY_MODE_NEW_ROOT, StaticParentSpanId: "span:other"}
			},
			wantField:  "spans[0].parent.static_parent_span_id",
			wantSubstr: "outside",
		},
		{
			name: "missing status policy",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Status = nil
			},
			wantField:  "spans[0].status",
			wantSubstr: "missing",
		},
		{
			name: "unspecified status mapping",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Status.Ok = StatusSetting_STATUS_SETTING_UNSPECIFIED
			},
			wantField:  "spans[0].status.ok",
			wantSubstr: "unspecified",
		},
		{
			name: "empty event ID",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Events[0].Id = ""
			},
			wantField:  "spans[0].events[0].id",
			wantSubstr: "empty",
		},
		{
			name: "empty event name",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Events[0].Name = ""
			},
			wantField:  "spans[0].events[0].name",
			wantSubstr: "empty",
		},
		{
			name: "unspecified event status",
			mutate: func(plan *GenerationPlan) {
				plan.Spans[0].Events[0].Statuses[0] = RuntimeStatus_RUNTIME_STATUS_UNSPECIFIED
			},
			wantField:  "spans[0].events[0].statuses[0]",
			wantSubstr: "unspecified",
		},
		{
			name: "unspecified log severity",
			mutate: func(plan *GenerationPlan) {
				plan.Logs[0].Severity = LogSeverity_LOG_SEVERITY_UNSPECIFIED
			},
			wantField:  "logs[0].severity",
			wantSubstr: "unspecified",
		},
		{
			name: "empty event name on log",
			mutate: func(plan *GenerationPlan) {
				plan.Logs[0].EventName = ""
			},
			wantField:  "logs[0].event_name",
			wantSubstr: "empty",
		},
		{
			name: "empty log ID",
			mutate: func(plan *GenerationPlan) {
				plan.Logs[0].Id = ""
			},
			wantField:  "logs[0].id",
			wantSubstr: "empty",
		},
		{
			name: "empty field key",
			mutate: func(plan *GenerationPlan) {
				plan.Logs[0].Fields[0].Key = ""
			},
			wantField:  "logs[0].fields[0].key",
			wantSubstr: "empty",
		},
		{
			name: "empty correlation field name",
			mutate: func(plan *GenerationPlan) {
				plan.Logs[0].CorrelationFields[0] = ""
			},
			wantField:  "logs[0].correlation_fields[0]",
			wantSubstr: "empty",
		},
		{
			name: "unspecified diagnostic severity",
			mutate: func(plan *GenerationPlan) {
				plan.Diagnostics[0].Severity = PlanSeverity_PLAN_SEVERITY_UNSPECIFIED
			},
			wantField:  "diagnostics[0].severity",
			wantSubstr: "unspecified",
		},
		{
			name: "empty diagnostic code",
			mutate: func(plan *GenerationPlan) {
				plan.Diagnostics[0].Code = ""
			},
			wantField:  "diagnostics[0].code",
			wantSubstr: "empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := clonePlan(t, fullGenerationPlan())
			tc.mutate(plan)
			violations := ValidateGenerationPlan(fullDocument(), plan)
			for _, violation := range violations {
				if violation.Field == tc.wantField && strings.Contains(violation.Message, tc.wantSubstr) {
					return
				}
			}
			t.Fatalf("expected violation at %s containing %q, got:\n%s", tc.wantField, tc.wantSubstr, formatViolations(violations))
		})
	}
}

// TestValidateGenerationPlanReportsEntityIDs ensures AC3: violations carry
// both a concrete field path and the offending entity ID.
func TestValidateGenerationPlanReportsEntityIDs(t *testing.T) {
	plan := clonePlan(t, fullGenerationPlan())
	plan.Metrics[0].Target = &TargetRef{Kind: TargetKind_TARGET_KIND_DEPENDENCY, Id: "endpoint:http:create-order"}
	plan.Spans[1].Id = plan.Metrics[0].Id

	violations := ValidateGenerationPlan(fullDocument(), plan)
	byField := map[string]PlanValidationError{}
	for _, violation := range violations {
		byField[violation.Field] = *violation
	}
	mismatch, ok := byField["metrics[0].target.kind"]
	if !ok || mismatch.EntityID != "endpoint:http:create-order" {
		t.Fatalf("kind mismatch violation must carry target entity ID: got %v", violations)
	}
	duplicate, ok := byField["spans[1].id"]
	if !ok || duplicate.EntityID != plan.Metrics[0].Id {
		t.Fatalf("duplicate ID violation must carry plan entity ID: got %v", violations)
	}
}

// TestValidateGenerationPlanDoesNotMutateInputs satisfies the NFR that the
// validator is read-only.
func TestValidateGenerationPlanDoesNotMutateInputs(t *testing.T) {
	document := fullDocument()
	plan := fullGenerationPlan()
	documentSnapshot := proto.Clone(document)
	planSnapshot := proto.Clone(plan)

	ValidateGenerationPlan(document, plan)

	if !proto.Equal(document, documentSnapshot) {
		t.Fatalf("validator mutated the document")
	}
	if !proto.Equal(plan, planSnapshot) {
		t.Fatalf("validator mutated the plan")
	}
}

// TestValidateGenerationPlanViolationOrderIsDeterministic ensures two
// calls report violations in the same order, independent of map iteration
// or input ordering quirks.
func TestValidateGenerationPlanViolationOrderIsDeterministic(t *testing.T) {
	plan := clonePlan(t, fullGenerationPlan())
	plan.Metrics[0].Target.Id = ""
	plan.Metrics[0].Value.Source = ValueSource_VALUE_SOURCE_UNSPECIFIED
	plan.Spans[0].Status.Ok = StatusSetting_STATUS_SETTING_UNSPECIFIED
	plan.Spans[0].Events[0].Name = ""
	plan.Logs[0].Severity = LogSeverity_LOG_SEVERITY_UNSPECIFIED

	document := fullDocument()
	first := ValidateGenerationPlan(document, plan)
	second := ValidateGenerationPlan(document, plan)
	if len(first) != len(second) {
		t.Fatalf("violation count differs between calls: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Field != second[i].Field {
			t.Fatalf("violation order differs at index %d: %q vs %q", i, first[i].Field, second[i].Field)
		}
	}
}

// TestValidateGenerationPlanResultIsFresh verifies every call returns a
// fresh slice that never aliases a previous result.
func TestValidateGenerationPlanResultIsFresh(t *testing.T) {
	plan := clonePlan(t, fullGenerationPlan())
	plan.Metrics[0].Target.Id = ""
	first := ValidateGenerationPlan(fullDocument(), plan)
	second := ValidateGenerationPlan(fullDocument(), plan)
	if len(first) == 0 {
		t.Fatalf("expected violations")
	}
	if &first[0] == &second[0] {
		t.Fatalf("consecutive calls returned the same backing slice")
	}
	first[0].Message = "tampered"
	if second[0].Message == "tampered" {
		t.Fatalf("mutating one result corrupted another")
	}
}

func violationFields(violations []*PlanValidationError) []string {
	fields := make([]string, 0, len(violations))
	for _, violation := range violations {
		fields = append(fields, violation.Field)
	}
	return fields
}

func containsField(fields []string, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}

func formatViolations(violations []*PlanValidationError) string {
	var builder strings.Builder
	for _, violation := range violations {
		builder.WriteString("  - ")
		builder.WriteString(violation.Error())
		builder.WriteString("\n")
	}
	return builder.String()
}

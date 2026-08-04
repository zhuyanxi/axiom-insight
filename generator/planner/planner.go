package planner

import (
	"context"
	"fmt"
	"slices"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/policy"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// Signal names used in stable plan IDs and reports.
const (
	SignalMetrics = "metrics"
	SignalTracing = "tracing"
	SignalLogging = "logging"
)

// SupportedIRSchemaVersion is the only ObservabilityDocument schema
// version this planner accepts.
const SupportedIRSchemaVersion = "v1"

// Planner coordinates the per-signal sub-planners. It holds no mutable
// state, so one Planner can plan many documents concurrently.
type Planner struct {
	metrics MetricsPlanner
	tracing TracingPlanner
	logging LoggingPlanner
}

// Options injects the per-signal sub-planners (extension point; no
// package-level registry). A nil planner for an enabled signal is a
// wiring error reported at Plan time.
type Options struct {
	Metrics MetricsPlanner
	Tracing TracingPlanner
	Logging LoggingPlanner
}

// New builds a Planner from injected sub-planners.
func New(options Options) *Planner {
	return &Planner{
		metrics: options.Metrics,
		tracing: options.Tracing,
		logging: options.Logging,
	}
}

// MetricsPlanner plans metric instruments from validated IR. P1-06/P1-07
// implement this interface.
type MetricsPlanner interface {
	PlanMetrics(ctx context.Context, input *SignalInput) (*MetricsResult, error)
}

// TracingPlanner plans span definitions from validated IR. P1-09/P1-10
// implement this interface.
type TracingPlanner interface {
	PlanTracing(ctx context.Context, input *SignalInput) (*TracingResult, error)
}

// LoggingPlanner plans structured log events from validated IR. P1-12
// implements this interface.
type LoggingPlanner interface {
	PlanLogging(ctx context.Context, input *SignalInput) (*LoggingResult, error)
}

// SignalInput carries everything a sub-planner needs: the validated IR,
// the entity index and the resolved policy. Sub-planners never modify
// the document.
type SignalInput struct {
	Document *observabilityv1.ObservabilityDocument
	Index    *Index
	Policy   policy.Policy
}

// MetricsResult is the output of one metrics planning call. Items are
// unmerged until the main planner sorts them.
type MetricsResult struct {
	Items       []*observabilityv1.MetricPlan
	Diagnostics []naming.Diagnostic
	// Skipped counts IR entities the planner could not map safely.
	Skipped int
}

// TracingResult is the output of one tracing planning call.
type TracingResult struct {
	Items       []*observabilityv1.SpanPlan
	Diagnostics []naming.Diagnostic
	Skipped     int
}

// LoggingResult is the output of one logging planning call.
type LoggingResult struct {
	Items       []*observabilityv1.LogPlan
	Diagnostics []naming.Diagnostic
	Skipped     int
}

// Plan validates the IR, runs every policy-enabled sub-planner and
// returns a fully sorted GenerationPlan, or nil and an error when any
// fatal condition (invalid IR, dangling reference, strict warning,
// cardinality failure, cancellation, wiring error) occurs. Warning
// diagnostics travel through the Report; the error never carries plan
// content.
func (p *Planner) Plan(ctx context.Context, document *observabilityv1.ObservabilityDocument, policy policy.Policy) (*observabilityv1.GenerationPlan, Report, error) {
	report := newReport(document)
	if err := contextError(ctx); err != nil {
		return nil, report, err
	}

	index, violations, err := validateDocument(ctx, document)
	if err != nil {
		return nil, report, err
	}
	if len(violations) > 0 {
		return nil, report, &InvalidIRError{violations: violations}
	}

	plan := &observabilityv1.GenerationPlan{
		SchemaVersion:         observabilityv1.GenerationPlanSchemaVersion,
		SourceIrSchemaVersion: document.GetSchemaVersion(),
		ServiceName:           document.GetService().GetName(),
	}
	input := &SignalInput{Document: document, Index: index, Policy: policy}

	if err := contextError(ctx); err != nil {
		return nil, report, err
	}
	var diagnostics []naming.Diagnostic

	if slices.Contains(policy.Signals, SignalMetrics) {
		result, planErr := p.planMetrics(ctx, input)
		if planErr != nil {
			return nil, report, planErr
		}
		plan.Metrics = result.Items
		diagnostics = append(diagnostics, result.Diagnostics...)
		report.Skipped.Metrics = result.Skipped
	}
	if err := contextError(ctx); err != nil {
		return nil, report, err
	}
	if slices.Contains(policy.Signals, SignalTracing) {
		result, planErr := p.planTracing(ctx, input)
		if planErr != nil {
			return nil, report, planErr
		}
		plan.Spans = result.Items
		diagnostics = append(diagnostics, result.Diagnostics...)
		report.Skipped.Spans = result.Skipped
	}
	if err := contextError(ctx); err != nil {
		return nil, report, err
	}
	if slices.Contains(policy.Signals, SignalLogging) {
		result, planErr := p.planLogging(ctx, input)
		if planErr != nil {
			return nil, report, planErr
		}
		plan.Logs = result.Items
		diagnostics = append(diagnostics, result.Diagnostics...)
		report.Skipped.Logs = result.Skipped
	}

	// Cross-sub-planner checks before merging: duplicate plan IDs, target
	// kind mismatches and unsupported binding sources all fail the plan.
	if violations := crossCheckItems(ctx, index, plan); len(violations) > 0 {
		return nil, report, &InvalidIRError{violations: violations}
	}

	sortPlanItems(plan)

	// Strict mode promotes generator warnings; error-level diagnostics
	// always fail.
	if err := naming.StrictError(policy.Strict, diagnostics); err != nil {
		return nil, report, err
	}

	if violations := observabilityv1.ValidateGenerationPlan(document, plan); len(violations) > 0 {
		return nil, report, fmt.Errorf("planner produced an invalid plan: %v", violations[0])
	}

	plan.Diagnostics = planDiagnostics(diagnostics)
	report.fill(plan, document, diagnostics)
	return plan, report, nil
}

func (p *Planner) planMetrics(ctx context.Context, input *SignalInput) (*MetricsResult, error) {
	if p.metrics == nil {
		return nil, fmt.Errorf("planner: %s: no metrics planner registered", SignalMetrics)
	}
	result, err := p.metrics.PlanMetrics(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("planner: %s: %w", SignalMetrics, err)
	}
	return result, nil
}

func (p *Planner) planTracing(ctx context.Context, input *SignalInput) (*TracingResult, error) {
	if p.tracing == nil {
		return nil, fmt.Errorf("planner: %s: no tracing planner registered", SignalTracing)
	}
	result, err := p.tracing.PlanTracing(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("planner: %s: %w", SignalTracing, err)
	}
	return result, nil
}

func (p *Planner) planLogging(ctx context.Context, input *SignalInput) (*LoggingResult, error) {
	if p.logging == nil {
		return nil, fmt.Errorf("planner: %s: no logging planner registered", SignalLogging)
	}
	result, err := p.logging.PlanLogging(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("planner: %s: %w", SignalLogging, err)
	}
	return result, nil
}

// crossCheckItems verifies plan-wide invariants before merging: every
// plan ID is unique across all signals, every target references an
// existing entity of the matching kind, and every value binding uses a
// supported source.
func crossCheckItems(ctx context.Context, index *Index, plan *observabilityv1.GenerationPlan) []ValidationError {
	var violations []ValidationError
	seen := make(map[string]bool, len(plan.Metrics)+len(plan.Spans)+len(plan.Logs))
	check := func(ownerID, field string, target *observabilityv1.TargetRef) {
		if target == nil {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: ownerID, Field: field,
				Message: "plan item has no target reference",
			})
			return
		}
		kind, ok := index.Kind(target.GetId())
		if !ok {
			violations = append(violations, ValidationError{
				Code: "GEN_DANGLING_REFERENCE", EntityID: ownerID, Field: field,
				Message: "plan target " + target.GetId() + " does not exist",
			})
			return
		}
		if kind != target.GetKind() {
			violations = append(violations, ValidationError{
				Code: "GEN_DANGLING_REFERENCE", EntityID: ownerID, Field: field,
				Message: "plan target " + target.GetId() + " kind does not match the referenced entity",
			})
		}
	}

	for _, metric := range plan.Metrics {
		if err := contextError(ctx); err != nil {
			return violations
		}
		if seen[metric.GetId()] {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: metric.GetId(), Field: "id",
				Message: "duplicate plan ID across signals",
			})
		}
		seen[metric.GetId()] = true
		check(metric.GetId(), "target", metric.GetTarget())
	}
	for _, span := range plan.Spans {
		if err := contextError(ctx); err != nil {
			return violations
		}
		if seen[span.GetId()] {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: span.GetId(), Field: "id",
				Message: "duplicate plan ID across signals",
			})
		}
		seen[span.GetId()] = true
		check(span.GetId(), "target", span.GetTarget())
	}
	for _, logPlan := range plan.Logs {
		if err := contextError(ctx); err != nil {
			return violations
		}
		if seen[logPlan.GetId()] {
			violations = append(violations, ValidationError{
				Code: "GEN_INVALID_IR", EntityID: logPlan.GetId(), Field: "id",
				Message: "duplicate plan ID across signals",
			})
		}
		seen[logPlan.GetId()] = true
		check(logPlan.GetId(), "target", logPlan.GetTarget())
	}
	return violations
}

// planDiagnostics converts naming diagnostics into the plan's own
// diagnostic list, deterministically ordered.
func planDiagnostics(diagnostics []naming.Diagnostic) []*observabilityv1.PlanDiagnostic {
	result := make([]*observabilityv1.PlanDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		severity := observabilityv1.PlanSeverity_PLAN_SEVERITY_ERROR
		if naming.IsWarning(diagnostic.Code) {
			severity = observabilityv1.PlanSeverity_PLAN_SEVERITY_WARNING
		}
		message := diagnostic.Message
		if diagnostic.TargetID != "" {
			message = diagnostic.TargetID + ": " + diagnostic.Field + ": " + message
		}
		result = append(result, &observabilityv1.PlanDiagnostic{
			Severity: severity,
			Code:     diagnostic.Code,
			Message:  message,
		})
	}
	return result
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

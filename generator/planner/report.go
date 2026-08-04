package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zhuyanxi/axiom-insight/generator/naming"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
)

// Report summarizes one planning run: input entity counts, per-signal
// item and skip counts, and diagnostic counts per code and severity. It
// never carries sensitive values.
type Report struct {
	Input    InputCounts
	Items    ItemCounts
	Skipped  SkippedCounts
	// GeneratorDiagnostics counts generator diagnostics by code, sorted.
	GeneratorDiagnostics []CodeCount
	// SourceDiagnostics counts Phase 0 IR diagnostics by code, sorted.
	SourceDiagnostics []CodeCount
	// SourceSeverity counts Phase 0 IR diagnostics by severity.
	SourceSeverity []SeverityCount
}

// InputCounts is the IR entity census.
type InputCounts struct {
	Functions    int
	Endpoints    int
	Dependencies int
	CallEdges    int
}

// ItemCounts is the generated item census per signal.
type ItemCounts struct {
	Metrics int
	Spans   int
	Logs    int
}

// SkippedCounts is the skipped-entity census per signal.
type SkippedCounts struct {
	Metrics int
	Spans   int
	Logs    int
}

// CodeCount pairs a diagnostic code with its occurrence count.
type CodeCount struct {
	Code  string
	Count int
}

// SeverityCount pairs a severity name with its occurrence count.
type SeverityCount struct {
	Severity string
	Count    int
}

// newReport builds the input census from the document.
func newReport(document *observabilityv1.ObservabilityDocument) Report {
	report := Report{}
	if document == nil {
		return report
	}
	report.Input = InputCounts{
		Functions:    len(document.Functions),
		Endpoints:    len(document.Endpoints),
		Dependencies: len(document.Dependencies),
		CallEdges:    len(document.CallEdges),
	}
	report.SourceDiagnostics = countCodes(document.Diagnostics, func(diagnostic *observabilityv1.Diagnostic) string {
		return diagnostic.GetCode()
	})
	report.SourceSeverity = countSeverities(document.Diagnostics)
	return report
}

// fill records item counts and generator diagnostic counts.
func (report *Report) fill(plan *observabilityv1.GenerationPlan, _ *observabilityv1.ObservabilityDocument, diagnostics []naming.Diagnostic) {
	report.Items = ItemCounts{
		Metrics: len(plan.Metrics),
		Spans:   len(plan.Spans),
		Logs:    len(plan.Logs),
	}
	counts := make(map[string]int, len(diagnostics))
	for _, diagnostic := range diagnostics {
		counts[diagnostic.Code]++
	}
	report.GeneratorDiagnostics = sortedCodeCounts(counts)
}

// String renders a deterministic one-line summary.
func (report Report) String() string {
	var builder strings.Builder
	fmt.Fprintf(&builder,
		"input functions=%d endpoints=%d dependencies=%d call_edges=%d; ",
		report.Input.Functions, report.Input.Endpoints, report.Input.Dependencies, report.Input.CallEdges)
	fmt.Fprintf(&builder,
		"items metrics=%d spans=%d logs=%d; ",
		report.Items.Metrics, report.Items.Spans, report.Items.Logs)
	fmt.Fprintf(&builder,
		"skipped metrics=%d spans=%d logs=%d; ",
		report.Skipped.Metrics, report.Skipped.Spans, report.Skipped.Logs)
	if len(report.GeneratorDiagnostics) > 0 {
		builder.WriteString("generator_diagnostics ")
		writeCodeCounts(&builder, report.GeneratorDiagnostics)
		builder.WriteString("; ")
	}
	if len(report.SourceDiagnostics) > 0 {
		builder.WriteString("source_diagnostics ")
		writeCodeCounts(&builder, report.SourceDiagnostics)
		builder.WriteString("; ")
	}
	for _, severity := range report.SourceSeverity {
		fmt.Fprintf(&builder, "%s=%d ", severity.Severity, severity.Count)
	}
	return strings.TrimSpace(builder.String())
}

func countCodes(diagnostics []*observabilityv1.Diagnostic, code func(*observabilityv1.Diagnostic) string) []CodeCount {
	counts := make(map[string]int, len(diagnostics))
	for _, diagnostic := range diagnostics {
		counts[code(diagnostic)]++
	}
	return sortedCodeCounts(counts)
}

func countSeverities(diagnostics []*observabilityv1.Diagnostic) []SeverityCount {
	counts := map[string]int{"info": 0, "warning": 0, "error": 0}
	for _, diagnostic := range diagnostics {
		switch diagnostic.GetSeverity() {
		case observabilityv1.DiagnosticSeverity_INFO:
			counts["info"]++
		case observabilityv1.DiagnosticSeverity_WARNING:
			counts["warning"]++
		case observabilityv1.DiagnosticSeverity_ERROR:
			counts["error"]++
		}
	}
	result := make([]SeverityCount, 0, 3)
	for _, name := range []string{"info", "warning", "error"} {
		result = append(result, SeverityCount{Severity: name, Count: counts[name]})
	}
	return result
}

func sortedCodeCounts(counts map[string]int) []CodeCount {
	result := make([]CodeCount, 0, len(counts))
	for code, count := range counts {
		result = append(result, CodeCount{Code: code, Count: count})
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Code < result[right].Code
	})
	return result
}

func writeCodeCounts(builder *strings.Builder, counts []CodeCount) {
	for index, count := range counts {
		if index > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(builder, "%s=%d", count.Code, count.Count)
	}
}

package naming

import (
	"fmt"
	"strings"

	"github.com/zhuyanxi/axiom-insight/generator/policy"
)

// Diagnostic is one stable, locatable rule violation produced by the
// naming or attribute policy. Messages never contain rejected values:
// they carry the target ID, the field path and the rule explanation only.
type Diagnostic struct {
	// Code is a stable message code (GEN_*).
	Code string
	// Signal is the affected signal: metrics, tracing or logging.
	Signal string
	// TargetID identifies the IR entity the rule applies to.
	TargetID string
	// Field locates the offending item, e.g. "name" or "attributes[0]".
	Field string
	// Message explains the rule without echoing any value.
	Message string
}

// DiagnosticList collects diagnostics deterministically.
type DiagnosticList struct {
	items []Diagnostic
}

// Add appends one diagnostic.
func (list *DiagnosticList) Add(code, signal, targetID, field, message string) {
	list.items = append(list.items, Diagnostic{
		Code: code, Signal: signal, TargetID: targetID, Field: field, Message: message,
	})
}

// Items returns the collected diagnostics. The returned slice must not be
// modified. A nil receiver returns nil.
func (list *DiagnosticList) Items() []Diagnostic {
	if list == nil {
		return nil
	}
	return list.items
}

// IsWarning reports whether a code is a warning that strict mode promotes
// to failure. Error-level codes always fail.
func IsWarning(code string) bool {
	switch code {
	case policy.CodeNameCollision,
		policy.CodeCardinalityBlocked,
		policy.CodeSensitiveValueDropped,
		policy.CodeUnsupportedEntity,
		policy.CodeIncompleteTarget:
		return true
	}
	return false
}

// StrictError converts warning diagnostics into a failure when strict
// mode is on. In non-strict mode it returns nil; warning diagnostics
// travel through the report instead. Error-level diagnostics always
// produce a failure.
func StrictError(strict bool, diagnostics []Diagnostic) error {
	var warnings []string
	for _, diagnostic := range diagnostics {
		if !IsWarning(diagnostic.Code) {
			return &StrictFailure{diagnostics: diagnostics}
		}
		if strict {
			warnings = append(warnings, formatDiagnostic(diagnostic))
		}
	}
	if !strict || len(warnings) == 0 {
		return nil
	}
	return &StrictFailure{diagnostics: diagnostics}
}

// StrictFailure reports that strict mode promoted warnings to failure.
type StrictFailure struct {
	diagnostics []Diagnostic
}

// Error implements error. It never contains rejected values.
func (failure *StrictFailure) Error() string {
	lines := make([]string, 0, len(failure.diagnostics))
	for _, diagnostic := range failure.diagnostics {
		lines = append(lines, formatDiagnostic(diagnostic))
	}
	return strings.Join(lines, "\n")
}

func formatDiagnostic(diagnostic Diagnostic) string {
	return fmt.Sprintf("%s: %s %s: %s: %s",
		diagnostic.Code, diagnostic.Signal, diagnostic.TargetID, diagnostic.Field, diagnostic.Message)
}

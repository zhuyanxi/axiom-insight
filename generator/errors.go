package generator

import "fmt"

// ValidationError describes one semantic violation found by the document
// validators. Field is a dotted path into the document (for example
// "metrics[2].attributes[0].key"); Document is the owning document type.
type ValidationError struct {
	Document string
	Field    string
	Message  string
}

// Error implements error.
func (violation *ValidationError) Error() string {
	return fmt.Sprintf("%s %s: %s", violation.Document, violation.Field, violation.Message)
}

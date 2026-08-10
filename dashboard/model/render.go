package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Render serializes a validated dashboard to canonical bytes: fixed
// struct field order, two-space indentation, trailing LF, no HTML
// escaping, no timestamps, random values, server IDs or host
// information. The document is validated first; invalid dashboards never
// render.
func Render(dashboard *Dashboard) ([]byte, error) {
	if violations := Validate(dashboard); len(violations) > 0 {
		return nil, fmt.Errorf("cannot render invalid dashboard: %w", violations[0])
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(dashboard); err != nil {
		return nil, fmt.Errorf("render dashboard: %w", err)
	}
	return buffer.Bytes(), nil
}

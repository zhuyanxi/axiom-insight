// Package schemacheck validates JSON documents against a deliberately small
// subset of JSON Schema (draft-07) that is sufficient for the generator
// v1 contracts in schemas/generator/v1. It exists so the contract test can
// confirm that the committed JSON Schema files agree with the Go validators
// without requiring a network fetch of a third-party validator.
//
// Supported keywords: $defs, $ref (local "#/$defs/..." only), type,
// properties, additionalProperties (boolean), required, items (single
// schema), enum, const, minItems, uniqueItems, minLength, pattern,
// minimum, maximum, exclusiveMinimum, exclusiveMaximum, oneOf, anyOf,
// allOf, not. Anything else in a schema is ignored; the generator schemas are
// tested to only use these keywords.
package schemacheck

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Validate checks docJSON against schemaJSON. It returns nil when the
// document is valid and a descriptive error otherwise.
func Validate(schemaJSON, docJSON []byte) error {
	schema := make(map[string]any)
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return fmt.Errorf("invalid schema JSON: %w", err)
	}
	var document any
	if err := json.Unmarshal(docJSON, &document); err != nil {
		return fmt.Errorf("invalid document JSON: %w", err)
	}
	validator := &validator{
		defs:   defsOf(schema),
		root:   schema,
		active: make(map[*subSchema]bool),
	}
	return validator.validate(schema, document, "$")
}

type subSchema struct {
	value map[string]any
}

type validator struct {
	defs   map[string]*subSchema
	root   map[string]any
	active map[*subSchema]bool
}

func defsOf(schema map[string]any) map[string]*subSchema {
	raw, ok := schema["$defs"].(map[string]any)
	if !ok {
		return nil
	}
	defs := make(map[string]*subSchema, len(raw))
	for name, value := range raw {
		mapping, ok := value.(map[string]any)
		if !ok {
			continue
		}
		defs[name] = &subSchema{value: mapping}
	}
	return defs
}

func (checker *validator) resolve(schema map[string]any) (*subSchema, error) {
	reference, ok := schema["$ref"].(string)
	if !ok {
		return nil, nil
	}
	if !strings.HasPrefix(reference, "#/$defs/") {
		return nil, fmt.Errorf("unsupported $ref %q: only local #/$defs/ references are supported", reference)
	}
	name := strings.TrimPrefix(reference, "#/$defs/")
	sub, ok := checker.defs[name]
	if !ok {
		return nil, fmt.Errorf("unknown $ref %q", reference)
	}
	return sub, nil
}

func (checker *validator) validate(schema map[string]any, document any, path string) error {
	if reference := schema["$ref"]; reference != nil {
		sub, err := checker.resolve(schema)
		if err != nil {
			return err
		}
		if sub == nil {
			return nil
		}
		if checker.active[sub] {
			return nil
		}
		checker.active[sub] = true
		defer delete(checker.active, sub)
		return checker.validate(sub.value, document, path)
	}

	if schemaType, ok := schema["type"].(string); ok {
		if err := checkType(schemaType, document); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	if err := checker.validateEnum(schema, document, path); err != nil {
		return err
	}
	if err := checker.validateStringConstraints(schema, document, path); err != nil {
		return err
	}
	if err := checker.validateNumberConstraints(schema, document, path); err != nil {
		return err
	}
	if err := checker.validateArrayConstraints(schema, document, path); err != nil {
		return err
	}
	if err := checker.validateObjectConstraints(schema, document, path); err != nil {
		return err
	}

	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if err := checker.validateCombinators(keyword, schema, document, path); err != nil {
			return err
		}
	}
	if raw, ok := schema["not"].(map[string]any); ok {
		if checker.validate(raw, document, path) == nil {
			return fmt.Errorf("%s: value matches forbidden schema", path)
		}
	}
	return nil
}

func (checker *validator) validateEnum(schema map[string]any, document any, path string) error {
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			if jsonEqual(candidate, document) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value not in enum", path)
		}
	}
	if constant, ok := schema["const"]; ok {
		if !jsonEqual(constant, document) {
			return fmt.Errorf("%s: value does not match const", path)
		}
	}
	return nil
}

func (checker *validator) validateStringConstraints(schema map[string]any, document any, path string) error {
	text, ok := document.(string)
	if !ok {
		return nil
	}
	if minimum, ok := schema["minLength"].(float64); ok {
		if float64(len([]rune(text))) < minimum {
			return fmt.Errorf("%s: string shorter than minLength", path)
		}
	}
	if pattern, ok := schema["pattern"].(string); ok {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid schema pattern %q: %w", pattern, err)
		}
		if !compiled.MatchString(text) {
			return fmt.Errorf("%s: string does not match pattern", path)
		}
	}
	return nil
}

func (checker *validator) validateNumberConstraints(schema map[string]any, document any, path string) error {
	number, ok := numberValue(document)
	if !ok {
		return nil
	}
	if minimum, ok := schema["minimum"].(float64); ok && number < minimum {
		return fmt.Errorf("%s: number below minimum", path)
	}
	if maximum, ok := schema["maximum"].(float64); ok && number > maximum {
		return fmt.Errorf("%s: number above maximum", path)
	}
	if minimum, ok := schema["exclusiveMinimum"].(float64); ok && number <= minimum {
		return fmt.Errorf("%s: number not above exclusiveMinimum", path)
	}
	if maximum, ok := schema["exclusiveMaximum"].(float64); ok && number >= maximum {
		return fmt.Errorf("%s: number not below exclusiveMaximum", path)
	}
	return nil
}

func (checker *validator) validateArrayConstraints(schema map[string]any, document any, path string) error {
	items, ok := document.([]any)
	if !ok {
		return nil
	}
	if minimum, ok := schema["minItems"].(float64); ok && float64(len(items)) < minimum {
		return fmt.Errorf("%s: array shorter than minItems", path)
	}
	if unique, ok := schema["uniqueItems"].(bool); ok && unique {
		for left := 0; left < len(items); left++ {
			for right := left + 1; right < len(items); right++ {
				if jsonEqual(items[left], items[right]) {
					return fmt.Errorf("%s: array items are not unique", path)
				}
			}
		}
	}
	if itemSchema, ok := schema["items"].(map[string]any); ok {
		for index, item := range items {
			if err := checker.validate(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (checker *validator) validateObjectConstraints(schema map[string]any, document any, path string) error {
	mapping, ok := document.(map[string]any)
	if !ok {
		return nil
	}
	for _, required := range stringList(schema["required"]) {
		if _, present := mapping[required]; !present {
			return fmt.Errorf("%s: missing required property %q", path, required)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	additional, hasAdditional := schema["additionalProperties"].(bool)
	for key, value := range mapping {
		propertySchema, known := properties[key]
		if known {
			if err := checker.validate(propertySchema.(map[string]any), value, path+"."+key); err != nil {
				return err
			}
			continue
		}
		if hasAdditional && !additional {
			return fmt.Errorf("%s: unknown property %q is not allowed", path, key)
		}
	}
	return nil
}

func (checker *validator) validateCombinators(keyword string, schema map[string]any, document any, path string) error {
	raw, ok := schema[keyword].([]any)
	if !ok {
		return nil
	}
	switch keyword {
	case "allOf":
		for index, sub := range raw {
			if err := checker.validate(sub.(map[string]any), document, path); err != nil {
				return fmt.Errorf("%s: allOf[%d]: %w", path, index, err)
			}
		}
	case "anyOf":
		for _, sub := range raw {
			if checker.validate(sub.(map[string]any), document, path) == nil {
				return nil
			}
		}
		return fmt.Errorf("%s: value matches no anyOf branch", path)
	case "oneOf":
		matches := 0
		for _, sub := range raw {
			if checker.validate(sub.(map[string]any), document, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s: value must match exactly one oneOf branch, matched %d", path, matches)
		}
	}
	return nil
}

func checkType(schemaType string, document any) error {
	switch schemaType {
	case "string":
		if _, ok := document.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := document.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "number":
		if _, ok := numberValue(document); !ok {
			return fmt.Errorf("expected number")
		}
	case "integer":
		value, ok := numberValue(document)
		if !ok || math.Trunc(value) != value {
			return fmt.Errorf("expected integer")
		}
	case "array":
		if _, ok := document.([]any); !ok {
			return fmt.Errorf("expected array")
		}
	case "object":
		if _, ok := document.(map[string]any); !ok {
			return fmt.Errorf("expected object")
		}
	case "null":
		if document != nil {
			return fmt.Errorf("expected null")
		}
	}
	return nil
}

func numberValue(document any) (float64, bool) {
	switch value := document.(type) {
	case float64:
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func stringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

// jsonEqual compares decoded JSON values semantically.
func jsonEqual(left, right any) bool {
	switch l := left.(type) {
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for key, lv := range l {
			rv, present := r[key]
			if !present || !jsonEqual(lv, rv) {
				return false
			}
		}
		return true
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return false
		}
		for index := range l {
			if !jsonEqual(l[index], r[index]) {
				return false
			}
		}
		return true
	case float64:
		r, ok := right.(float64)
		return ok && l == r
	case string:
		r, ok := right.(string)
		return ok && l == r
	case bool:
		r, ok := right.(bool)
		return ok && l == r
	case nil:
		return right == nil
	}
	return false
}

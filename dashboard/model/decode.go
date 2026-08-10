package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
)

// Decode strictly parses a dashboard document into the typed model. It
// rejects unknown fields, duplicate mapping keys, non-finite numbers,
// documents over MaxDocumentBytes, nesting over MaxDepth, invalid UTF-8,
// `__inputs`/`__requires` and any unsupported field, always with a JSON
// path in the error.
func Decode(data []byte) (*Dashboard, error) {
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("dashboard: document exceeds %d bytes", MaxDocumentBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("dashboard: invalid UTF-8")
	}
	value, err := parseValue(data, "dashboard", "$", 0)
	if err != nil {
		return nil, err
	}
	jsonData, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("dashboard: internal conversion failed: %w", err)
	}
	jsonDecoder := json.NewDecoder(bytes.NewReader(jsonData))
	jsonDecoder.DisallowUnknownFields()
	dashboard := new(Dashboard)
	if err := jsonDecoder.Decode(dashboard); err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}
	return dashboard, nil
}

// parseValue walks the JSON token stream building a strictly typed
// generic value, rejecting duplicate keys and non-finite numbers, and
// bounding nesting depth.
func parseValue(data []byte, documentName, path string, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("%s: %s: nesting exceeds %d levels", documentName, path, MaxDepth)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: %s: invalid JSON: %w", documentName, path, err)
	}
	return parseToken(decoder, token, documentName, path, depth)
}

func parseToken(decoder *json.Decoder, token json.Token, documentName, path string, depth int) (any, error) {
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			return parseObject(decoder, documentName, path, depth)
		case '[':
			return parseArray(decoder, documentName, path, depth)
		default:
			return nil, fmt.Errorf("%s: %s: unexpected delimiter %q", documentName, path, value)
		}
	case string:
		return value, nil
	case bool:
		return value, nil
	case nil:
		return nil, nil
	case json.Number:
		number, err := strconv.ParseFloat(value.String(), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, fmt.Errorf("%s: %s: non-finite number %q is not allowed", documentName, path, value.String())
		}
		return value, nil
	default:
		return nil, fmt.Errorf("%s: %s: unsupported token %T", documentName, path, token)
	}
}

func parseObject(decoder *json.Decoder, documentName, path string, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("%s: %s: nesting exceeds %d levels", documentName, path, MaxDepth)
	}
	result := make(map[string]any)
	seen := make(map[string]bool)
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: invalid JSON: %w", documentName, path, err)
		}
		if delimiter, ok := token.(json.Delim); ok && delimiter == '}' {
			return result, nil
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("%s: %s: mapping keys must be strings", documentName, path)
		}
		if key == "__inputs" || key == "__requires" {
			return nil, fmt.Errorf("%s: %s: unsupported field %q", documentName, path, key)
		}
		if seen[key] {
			return nil, fmt.Errorf("%s: %s: duplicate key %q", documentName, path, key)
		}
		seen[key] = true
		child, err := parseValueFrom(decoder, documentName, path+"."+key, depth+1)
		if err != nil {
			return nil, err
		}
		result[key] = child
	}
}

func parseArray(decoder *json.Decoder, documentName, path string, depth int) (any, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("%s: %s: nesting exceeds %d levels", documentName, path, MaxDepth)
	}
	result := make([]any, 0)
	index := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %s: invalid JSON: %w", documentName, path, err)
		}
		if delimiter, ok := token.(json.Delim); ok && delimiter == ']' {
			return result, nil
		}
		child, err := parseToken(decoder, token, documentName, fmt.Sprintf("%s[%d]", path, index), depth+1)
		if err != nil {
			return nil, err
		}
		result = append(result, child)
		index++
	}
}

// parseValueFrom reads the next value from the decoder at the given
// depth.
func parseValueFrom(decoder *json.Decoder, documentName, path string, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("%s: %s: unexpected end of document", documentName, path)
		}
		return nil, fmt.Errorf("%s: %s: invalid JSON: %w", documentName, path, err)
	}
	return parseToken(decoder, token, documentName, path, depth)
}


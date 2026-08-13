package pipeline

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"

	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// maxDiffEntries bounds a semantic diff so a diverged fixture reports a
// deterministic, bounded set of differences instead of exploding.
const maxDiffEntries = 100

// DiffEntry is one semantic difference between two dashboard documents.
// Path is a dotted JSON path; PanelID locates the owning panel (0 when
// the difference is outside a panel); Expected and Actual are the two
// scalar values rendered for the report.
type DiffEntry struct {
	Path     string
	PanelID  int
	Expected string
	Actual   string
}

// panelPathPattern matches a canonical panel path segment
// ("panels[<n>]" or "rows[<n>].panels[<m>]") whose object carries the
// owning panel ID.
var panelPathPattern = regexp.MustCompile(`panels\[(\d+)\]$`)

// Diff compares two dashboard documents semantically after strict
// decode. Both documents must decode and validate; the walk reports
// differences with stable paths and panel context instead of raw bytes,
// so a Golden failure names the offending panel and query metadata.
// Output is capped at maxDiffEntries.
func Diff(expected, actual []byte) ([]DiffEntry, error) {
	expectedModel, err := modelDecodeForDiff(expected)
	if err != nil {
		return nil, fmt.Errorf("pipeline: diff: expected document: %w", err)
	}
	actualModel, err := modelDecodeForDiff(actual)
	if err != nil {
		return nil, fmt.Errorf("pipeline: diff: actual document: %w", err)
	}
	var entries []DiffEntry
	diffValue("$", expectedModel, actualModel, 0, &entries)
	return entries, nil
}

// modelDecodeForDiff strictly decodes one document into a generic tree
// with canonical key order.
func modelDecodeForDiff(data []byte) (any, error) {
	dashboard, err := model.Decode(data)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(dashboard)
	if err != nil {
		return nil, fmt.Errorf("re-marshal failed: %w", err)
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("unmarshal failed: %w", err)
	}
	return value, nil
}

// diffValue walks two generic trees, emitting one DiffEntry per differing
// leaf. panelID is the owning panel ID discovered while descending a
// canonical "panels[n]" path; it is threaded through arrays and scalars
// so every difference inside a panel names its panel.
func diffValue(path string, expected, actual any, panelID int, entries *[]DiffEntry) {
	if len(*entries) >= maxDiffEntries {
		return
	}
	expectedMap, expectedIsMap := expected.(map[string]any)
	actualMap, actualIsMap := actual.(map[string]any)
	if expectedIsMap && actualIsMap {
		if panelPathPattern.MatchString(path) {
			if id, ok := mapPanelID(expectedMap); ok {
				panelID = id
			}
		}
		keys := sortedUnionKeys(expectedMap, actualMap)
		for _, key := range keys {
			expectedValue, expectedPresent := expectedMap[key]
			actualValue, actualPresent := actualMap[key]
			if expectedPresent != actualPresent {
				appendEntry(entries, DiffEntry{
					Path: path + "." + key, PanelID: panelID,
					Expected: scalarText(expectedValue, expectedPresent),
					Actual:   scalarText(actualValue, actualPresent),
				})
				continue
			}
			if !expectedPresent {
				continue
			}
			diffValue(path+"."+key, expectedValue, actualValue, panelID, entries)
		}
		return
	}

	expectedArray, expectedIsArray := expected.([]any)
	actualArray, actualIsArray := actual.([]any)
	if expectedIsArray && actualIsArray {
		length := len(expectedArray)
		if len(actualArray) > length {
			length = len(actualArray)
		}
		for index := 0; index < length; index++ {
			childPath := path + "[" + strconv.Itoa(index) + "]"
			if index >= len(expectedArray) || index >= len(actualArray) {
				var expectedValue, actualValue any
				if index < len(expectedArray) {
					expectedValue = expectedArray[index]
				}
				if index < len(actualArray) {
					actualValue = actualArray[index]
				}
				appendEntry(entries, DiffEntry{
					Path: childPath, PanelID: panelID,
					Expected: scalarText(expectedValue, index < len(expectedArray)),
					Actual:   scalarText(actualValue, index < len(actualArray)),
				})
				continue
			}
			diffValue(childPath, expectedArray[index], actualArray[index], panelID, entries)
		}
		return
	}

	if !reflect.DeepEqual(expected, actual) {
		appendEntry(entries, DiffEntry{
			Path: path, PanelID: panelID,
			Expected: scalarText(expected, true), Actual: scalarText(actual, true),
		})
	}
}

func appendEntry(entries *[]DiffEntry, entry DiffEntry) {
	if len(*entries) < maxDiffEntries {
		*entries = append(*entries, entry)
	}
}

// mapPanelID reads the owning panel ID out of a panel object.
func mapPanelID(object map[string]any) (int, bool) {
	raw, present := object["id"]
	if !present {
		return 0, false
	}
	switch value := raw.(type) {
	case json.Number:
		id, err := value.Int64()
		if err != nil {
			return 0, false
		}
		return int(id), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

// sortedUnionKeys returns the sorted union of two object key sets so the
// diff never depends on map iteration order.
func sortedUnionKeys(left, right map[string]any) []string {
	seen := make(map[string]bool, len(left)+len(right))
	for key := range left {
		seen[key] = true
	}
	for key := range right {
		seen[key] = true
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// scalarText renders a value for the report without echoing raw bytes;
// missing values render as "<absent>".
func scalarText(value any, present bool) string {
	if !present {
		return "<absent>"
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(payload)
}

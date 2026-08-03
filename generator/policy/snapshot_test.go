package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// updateSnapshotEnv switches snapshot regeneration on for this test only.
// Normal test runs never rewrite the committed snapshot.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

// snapshot holds the fully resolved default policy. Any change to the
// defaults requires an explicit review because the snapshot file is
// committed.
type snapshot struct {
	OutputDir                 string    `json:"output_dir"`
	Signals                   []string  `json:"signals"`
	Strict                    bool      `json:"strict"`
	Namespace                 string    `json:"metrics.namespace"`
	HistogramBucketsSeconds   []float64 `json:"metrics.histogram_buckets_seconds"`
	IncludeInFlightGauges     bool      `json:"metrics.include_in_flight_gauges"`
	MaxInstruments            int64     `json:"metrics.max_instruments"`
	MaxEstimatedSeries        int64     `json:"metrics.max_estimated_series"`
	SummariesEnabled          bool      `json:"metrics.summaries.enabled"`
	SummariesQuantiles        []float64 `json:"metrics.summaries.quantiles"`
	IncludeInternalCalls      bool      `json:"tracing.include_internal_calls"`
	RecordExceptionEvents     bool      `json:"tracing.record_exception_events"`
	SemanticConventionsVersion string   `json:"tracing.semantic_conventions_version"`
	EmitStartEvents           bool      `json:"logging.emit_start_events"`
	EmitCompletionEvents      bool      `json:"logging.emit_completion_events"`
	EmitDependencyErrors      bool      `json:"logging.emit_dependency_errors"`
	CorrelationFields         []string  `json:"logging.correlation_fields"`
	RedactFields              []string  `json:"logging.redact_fields"`
	Digest                    string    `json:"digest"`
}

func currentSnapshot() snapshot {
	resolved, err := Resolve(nil, nil)
	if err != nil {
		panic(err)
	}
	return snapshot{
		OutputDir:                  resolved.OutputDir,
		Signals:                    resolved.Signals,
		Strict:                     resolved.Strict,
		Namespace:                  resolved.Metrics.Namespace,
		HistogramBucketsSeconds:    resolved.Metrics.HistogramBucketsSeconds,
		IncludeInFlightGauges:      resolved.Metrics.IncludeInFlightGauges,
		MaxInstruments:             resolved.Metrics.MaxInstruments,
		MaxEstimatedSeries:         resolved.Metrics.MaxEstimatedSeries,
		SummariesEnabled:           resolved.Metrics.Summaries.Enabled,
		SummariesQuantiles:         resolved.Metrics.Summaries.Quantiles,
		IncludeInternalCalls:       resolved.Tracing.IncludeInternalCalls,
		RecordExceptionEvents:      resolved.Tracing.RecordExceptionEvents,
		SemanticConventionsVersion: resolved.Tracing.SemanticConventionsVersion,
		EmitStartEvents:            resolved.Logging.EmitStartEvents,
		EmitCompletionEvents:       resolved.Logging.EmitCompletionEvents,
		EmitDependencyErrors:       resolved.Logging.EmitDependencyErrors,
		CorrelationFields:          resolved.Logging.CorrelationFields,
		RedactFields:               resolved.Logging.RedactFields,
		Digest:                     resolved.Digest(),
	}
}

// TestDefaultPolicySnapshot fixes the default policy bytes. A default
// change must be reviewed and the snapshot regenerated with
// SI_UPDATE_GOLDEN=1.
func TestDefaultPolicySnapshot(t *testing.T) {
	contents, err := json.MarshalIndent(currentSnapshot(), "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	contents = append(contents, '\n')

	snapshotPath := filepath.Join("testdata", "default_policy.json")
	if os.Getenv(updateSnapshotEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, contents, 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		t.Logf("updated snapshot %s", snapshotPath)
		return
	}
	expected, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatalf("read snapshot %s (set %s=1 to regenerate): %v", snapshotPath, updateSnapshotEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("default policy differs from snapshot %s; set %s=1 to regenerate", snapshotPath, updateSnapshotEnv)
	}
}

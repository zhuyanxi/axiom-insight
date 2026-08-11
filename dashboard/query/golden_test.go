package query

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
)

// updateSnapshotEnv switches snapshot regeneration on for this test only.
const updateSnapshotEnv = "SI_UPDATE_GOLDEN"

// queryGolden pins every deterministic P2-05 output for the full-item
// fixture: rendered PromQL, metadata, trace links and diagnostics. Any
// change to the query builders, the fixed quantiles, the error-status
// pattern or the rate interval must be reviewed and the snapshot
// regenerated with SI_UPDATE_GOLDEN=1.
type queryGolden struct {
	HashVersion        string             `json:"hash_version"`
	RateInterval       string             `json:"rate_interval"`
	ErrorStatusPattern string             `json:"error_status_pattern"`
	Queries            []goldenQuery      `json:"queries"`
	TraceLinks         []goldenTraceLink  `json:"trace_links"`
	Diagnostics        []goldenDiagnostic `json:"diagnostics"`
}

type goldenQuery struct {
	CanonicalKey string         `json:"canonical_key"`
	Kind         QueryKind      `json:"kind"`
	ItemID       string         `json:"item_id"`
	Purpose      string         `json:"purpose"`
	PlanIDs      []string       `json:"plan_ids"`
	Expr         string         `json:"expr"`
	Metadata     goldenMetadata `json:"metadata"`
}

type goldenMetadata struct {
	Kind               QueryKind `json:"kind"`
	CanonicalKey       string    `json:"canonical_key"`
	PlanIDs            []string  `json:"plan_ids"`
	Provenance         []string  `json:"provenance"`
	RateInterval       string    `json:"rate_interval,omitempty"`
	Quantiles          []float64 `json:"quantiles,omitempty"`
	ErrorStatusPattern string    `json:"error_status_pattern,omitempty"`
	OperationValues    []string  `json:"operation_values,omitempty"`
	HashVersion        string    `json:"hash_version"`
}

type goldenTraceLink struct {
	CanonicalKey       string   `json:"canonical_key"`
	ItemID             string   `json:"item_id"`
	PlanIDs            []string `json:"plan_ids"`
	DatasourceVariable string   `json:"datasource_variable"`
	ServiceName        string   `json:"service_name"`
	Operation          string   `json:"operation"`
	SpanName           string   `json:"span_name"`
}

type goldenDiagnostic struct {
	Code     string `json:"code"`
	TargetID string `json:"target_id"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

func currentQueryGolden(t *testing.T) queryGolden {
	item := fullItem()
	policy := resolvePolicy(t)
	plans, links, diagnostics := PlanItemQueries(item, "payment", policy)

	golden := queryGolden{
		HashVersion: dashboard.HashVersion, RateInterval: policy.RateInterval,
		ErrorStatusPattern: ErrorStatusPattern,
	}
	for _, plan := range plans {
		expression, err := Render(plan.Expression)
		if err != nil {
			panic(err)
		}
		golden.Queries = append(golden.Queries, goldenQuery{
			CanonicalKey: plan.CanonicalKey,
			Kind:         plan.Kind,
			ItemID:       plan.ItemID,
			Purpose:      plan.Purpose,
			PlanIDs:      plan.PlanIDs,
			Expr:         expression,
			Metadata: goldenMetadata{
				Kind: plan.Metadata.Kind, CanonicalKey: plan.Metadata.CanonicalKey,
				PlanIDs: plan.Metadata.PlanIDs, Provenance: plan.Metadata.Provenance,
				RateInterval: plan.Metadata.RateInterval, Quantiles: plan.Metadata.Quantiles,
				ErrorStatusPattern: plan.Metadata.ErrorStatusPattern,
				OperationValues:    plan.Metadata.OperationValues,
				HashVersion:        plan.Metadata.HashVersion,
			},
		})
	}
	for _, link := range links {
		golden.TraceLinks = append(golden.TraceLinks, goldenTraceLink{
			CanonicalKey: link.CanonicalKey, ItemID: link.ItemID,
			PlanIDs: link.PlanIDs, DatasourceVariable: link.DatasourceVariable,
			ServiceName: link.ServiceName, Operation: link.Operation, SpanName: link.SpanName,
		})
	}
	for _, diagnostic := range diagnostics {
		golden.Diagnostics = append(golden.Diagnostics, goldenDiagnostic{
			Code: diagnostic.Code, TargetID: diagnostic.TargetID,
			Field: diagnostic.Field, Message: diagnostic.Message,
		})
	}
	return golden
}

// TestQueryGolden fixes the deterministic query bytes (DoD: rerun shows
// no diff). Regenerate with SI_UPDATE_GOLDEN=1.
func TestQueryGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentQueryGolden(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "query_plans_golden.json")
	if os.Getenv(updateSnapshotEnv) != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, contents, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (set %s=1 to regenerate): %v", goldenPath, updateSnapshotEnv, err)
	}
	if string(contents) != string(expected) {
		t.Fatalf("query plans differ from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

// TestTraceLinkGolden fixes the trace-link model bytes.
func TestTraceLinkGolden(t *testing.T) {
	item := fullItem()
	_, links, _ := PlanItemQueries(item, "payment", resolvePolicy(t))
	if len(links) != 1 {
		t.Fatalf("expected 1 trace link, got %d", len(links))
	}
	link := links[0]
	if link.DatasourceVariable != "datasource" {
		t.Errorf("datasource variable = %q, want the policy-fixed datasource", link.DatasourceVariable)
	}
	if link.ServiceName != "payment" || link.Operation != "get" || link.SpanName != "GET /users" {
		t.Errorf("link model = %+v", link)
	}
	if link.PlanIDs[0] != "s_span" {
		t.Errorf("link plan IDs = %v", link.PlanIDs)
	}
}

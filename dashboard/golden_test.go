package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// idGolden pins every deterministic P2-04 output for the fixed catalog
// fixture: UID, panel IDs, refIds, disambiguated titles and the grid.
// HashVersion is included so a deliberate hash change is an explicit,
// reviewed snapshot diff.
type idGolden struct {
	HashVersion string            `json:"hash_version"`
	UID         string            `json:"uid"`
	PanelIDs    map[string]int64  `json:"panel_ids"`
	RefIDs      map[string]string `json:"ref_ids"`
	Titles      map[string]string `json:"titles"`
	Rows        []idGoldenRow     `json:"rows"`
}

type idGoldenRow struct {
	Category string          `json:"category"`
	RowID    int64           `json:"row_id"`
	Y        int             `json:"y"`
	Panels   []idGoldenPlace `json:"panels"`
}

type idGoldenPlace struct {
	ID int64 `json:"id"`
	X  int   `json:"x"`
	Y  int   `json:"y"`
	W  int   `json:"w"`
	H  int   `json:"h"`
}

func currentIDGolden() idGolden {
	fixture := permutationFixture()
	result := fixture.compute()

	golden := idGolden{
		HashVersion: HashVersion,
		UID:         result.uid,
		PanelIDs:    result.panelIDs,
		RefIDs:      result.refIDs,
		Titles:      result.titles,
	}
	for _, category := range CategoryOrder {
		row, present := result.rowIDs[category]
		if !present {
			continue
		}
		goldenRow := idGoldenRow{Category: string(category), RowID: row, Y: result.rowY[category]}
		for id, placement := range result.positions[category] {
			goldenRow.Panels = append(goldenRow.Panels, idGoldenPlace{
				ID: id, X: placement.X, Y: placement.Y, W: placement.W, H: placement.H,
			})
		}
		sortGoldenPlaces(goldenRow.Panels)
		golden.Rows = append(golden.Rows, goldenRow)
	}
	return golden
}

func sortGoldenPlaces(places []idGoldenPlace) {
	for index := 1; index < len(places); index++ {
		for position := index; position > 0 && places[position].ID < places[position-1].ID; position-- {
			places[position], places[position-1] = places[position-1], places[position]
		}
	}
}

// TestDeterministicIDGolden fixes the deterministic ID/layout bytes. Any
// change to the hash inputs, collision strategy, UID format or layout
// rules must be reviewed and the snapshot regenerated with
// SI_UPDATE_GOLDEN=1 (DoD: rerun shows no diff).
func TestDeterministicIDGolden(t *testing.T) {
	contents, err := json.MarshalIndent(currentIDGolden(), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	contents = append(contents, '\n')

	goldenPath := filepath.Join("testdata", "deterministic_ids_golden.json")
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
		t.Fatalf("deterministic IDs differ from golden %s; set %s=1 to regenerate", goldenPath, updateSnapshotEnv)
	}
}

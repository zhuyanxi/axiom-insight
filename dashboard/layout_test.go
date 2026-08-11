package dashboard

import (
	"reflect"
	"testing"
)

// TestPlanGridFixedWidths verifies the fixed widths: stat 6, time-series
// 12, table 24, wrapping at 24 columns.
func TestPlanGridFixedWidths(t *testing.T) {
	panels := []GridPanel{
		{ID: 1, Width: PanelWidthStat, Height: 8},
		{ID: 2, Width: PanelWidthStat, Height: 8},
		{ID: 3, Width: PanelWidthTimeSeries, Height: 8},
		{ID: 4, Width: PanelWidthTable, Height: 8},
	}
	rows := PlanGrid([]CategoryLayout{{Category: CategoryHTTP, RowID: 100, Panels: panels}})
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	placements := rows[0].Panels
	expectedX := []int{0, 6, 12, 0}
	expectedW := []int{6, 6, 12, 24}
	for index, placement := range placements {
		if placement.X != expectedX[index] {
			t.Errorf("panel %d X = %d, want %d", placement.ID, placement.X, expectedX[index])
		}
		if placement.W != expectedW[index] {
			t.Errorf("panel %d W = %d, want %d", placement.ID, placement.W, expectedW[index])
		}
	}
}

// TestPlanGridRowStacking stacks category blocks top to bottom: the next
// row panel starts below the previous block (row panel + panels).
func TestPlanGridRowStacking(t *testing.T) {
	stat := func(id int64) GridPanel { return GridPanel{ID: id, Width: PanelWidthStat, Height: 8} }
	rows := PlanGrid([]CategoryLayout{
		{Category: CategoryServiceOverview, RowID: 1, Panels: []GridPanel{stat(10), stat(11)}},
		{Category: CategoryHTTP, RowID: 2, Panels: []GridPanel{stat(20)}},
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Category != CategoryServiceOverview || rows[0].Y != 0 {
		t.Errorf("first row = %s@%d, want service_overview@0", rows[0].Category, rows[0].Y)
	}
	// Overview block: row panel + one 8-row line = 9 rows.
	if rows[1].Y != 9 {
		t.Errorf("second row Y = %d, want 9", rows[1].Y)
	}
	for _, row := range rows {
		for _, placement := range row.Panels {
			if placement.Y != row.Y+1 {
				t.Errorf("panel %d Y = %d, want row Y+1 = %d", placement.ID, placement.Y, row.Y+1)
			}
		}
	}
}

// TestPlanGridWrap wraps a line at 24 columns and advances the next line
// by the previous line's height.
func TestPlanGridWrap(t *testing.T) {
	panels := []GridPanel{
		{ID: 1, Width: PanelWidthStat, Height: 8},
		{ID: 2, Width: PanelWidthStat, Height: 8},
		{ID: 3, Width: PanelWidthStat, Height: 8},
		{ID: 4, Width: PanelWidthStat, Height: 8},
		{ID: 5, Width: PanelWidthStat, Height: 8},
	}
	rows := PlanGrid([]CategoryLayout{{Category: CategoryHTTP, RowID: 1, Panels: panels}})
	placements := rows[0].Panels
	for index, wantX := range []int{0, 6, 12, 18, 0} {
		if placements[index].X != wantX {
			t.Errorf("panel %d X = %d, want %d", placements[index].ID, placements[index].X, wantX)
		}
	}
	if placements[4].Y != 1+8 {
		t.Errorf("wrapped panel Y = %d, want 9", placements[4].Y)
	}
}

// TestPlanGridEmptyCategories is AC3: a catalog with only Overview and
// HTTP panels yields exactly those two rows; Kafka, Database, Cache and
// RPC contribute no rows, no panels and no holes.
func TestPlanGridEmptyCategories(t *testing.T) {
	stat := func(id int64) GridPanel { return GridPanel{ID: id, Width: PanelWidthStat, Height: 8} }
	input := []CategoryLayout{
		{Category: CategoryKafka, RowID: 30, Panels: nil},
		{Category: CategoryDatabase, RowID: 40, Panels: []GridPanel{}},
		{Category: CategoryCache, RowID: 50},
		{Category: CategoryRPC, RowID: 60, Panels: nil},
		{Category: CategoryHTTP, RowID: 20, Panels: []GridPanel{stat(21)}},
		{Category: CategoryServiceOverview, RowID: 10, Panels: []GridPanel{stat(11)}},
	}
	rows := PlanGrid(input)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Category != CategoryServiceOverview || rows[1].Category != CategoryHTTP {
		t.Errorf("row order = [%s, %s], want [service_overview, http]", rows[0].Category, rows[1].Category)
	}
	if rows[0].Y != 0 {
		t.Errorf("first row Y = %d, want 0", rows[0].Y)
	}
}

// TestPlanGridUnknownCategory skips categories outside the fixed order.
func TestPlanGridUnknownCategory(t *testing.T) {
	rows := PlanGrid([]CategoryLayout{{
		Category: Category("mystery"),
		RowID:    1,
		Panels:   []GridPanel{{ID: 1, Width: PanelWidthStat, Height: 8}},
	}})
	if len(rows) != 0 {
		t.Errorf("unknown category must be skipped, got %d rows", len(rows))
	}
}

// TestPlanGridClamp bounds extents to [1, 24].
func TestPlanGridClamp(t *testing.T) {
	rows := PlanGrid([]CategoryLayout{{
		Category: CategoryHTTP,
		RowID:    1,
		Panels: []GridPanel{
			{ID: 1, Width: 0, Height: 0},
			{ID: 2, Width: 100, Height: -5},
		},
	}})
	placements := rows[0].Panels
	if placements[0].W != 1 || placements[0].H != 1 {
		t.Errorf("clamp up: panel 1 = %+v, want W=1 H=1", placements[0])
	}
	if placements[1].W != GridColumns || placements[1].H != 1 {
		t.Errorf("clamp down: panel 2 = %+v, want W=24 H=1", placements[1])
	}
}

// TestPlanGridOrderIndependent places panels by ascending ID regardless of
// input order (AC1).
func TestPlanGridOrderIndependent(t *testing.T) {
	panels := []GridPanel{
		{ID: 3, Width: PanelWidthStat, Height: 8},
		{ID: 1, Width: PanelWidthStat, Height: 8},
		{ID: 2, Width: PanelWidthTimeSeries, Height: 8},
	}
	first := PlanGrid([]CategoryLayout{{Category: CategoryHTTP, RowID: 1, Panels: panels}})
	second := PlanGrid([]CategoryLayout{{Category: CategoryHTTP, RowID: 1, Panels: []GridPanel{panels[2], panels[0], panels[1]}}})
	byID := make(map[int64]GridPlacement)
	for _, placement := range first[0].Panels {
		byID[placement.ID] = placement
	}
	for _, placement := range second[0].Panels {
		if byID[placement.ID] != placement {
			t.Errorf("panel %d geometry depends on input order", placement.ID)
		}
	}
}

// TestPlanGridEmptyInput yields no rows.
func TestPlanGridEmptyInput(t *testing.T) {
	if rows := PlanGrid(nil); len(rows) != 0 {
		t.Errorf("expected no rows, got %d", len(rows))
	}
}

// TestPlanGridDeterministic reruns identical input and compares byte-wise.
func TestPlanGridDeterministic(t *testing.T) {
	panels := []GridPanel{
		{ID: 1, Width: PanelWidthStat, Height: 8},
		{ID: 2, Width: PanelWidthTimeSeries, Height: 8},
	}
	input := []CategoryLayout{{Category: CategoryHTTP, RowID: 1, Panels: panels}}
	first := PlanGrid(input)
	second := PlanGrid(input)
	if len(first) != len(second) {
		t.Fatalf("row counts differ: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if !reflect.DeepEqual(first[index], second[index]) {
			t.Errorf("row %d differs between runs", index)
		}
	}
}

// TestPlanGridProperty runs a fixed-seed LCG over 400 random layouts and
// checks the invariants from the story: 0 <= X, X+W <= 24, 1 <= W <= 24,
// and no two panels within a category overlap. The generator is local so
// the test never touches global RNG.
func TestPlanGridProperty(t *testing.T) {
	random := newGridLCG(0x5eed)
	for round := 0; round < 400; round++ {
		var input []CategoryLayout
		for _, category := range CategoryOrder {
			if random.next()%3 == 0 {
				continue // category absent
			}
			count := int(random.next() % 13)
			if count == 0 {
				input = append(input, CategoryLayout{Category: category, RowID: int64(random.next())})
				continue // category present but empty
			}
			var panels []GridPanel
			for index := 0; index < count; index++ {
				panels = append(panels, GridPanel{
					ID:     int64(random.next() % 100000),
					Width:  int(int64(random.next()%34)) - 3, // -3 .. 30
					Height: int(int64(random.next()%34)) - 3,
				})
			}
			input = append(input, CategoryLayout{Category: category, RowID: int64(random.next()), Panels: panels})
		}
		rows := PlanGrid(input)
		previousBottom := 0
		seenCategories := make(map[Category]bool)
		for _, row := range rows {
			if row.Y < previousBottom {
				t.Fatalf("round %d: row %s at Y=%d overlaps the previous block (bottom %d)", round, row.Category, row.Y, previousBottom)
			}
			if seenCategories[row.Category] {
				t.Fatalf("round %d: category %s appears twice", round, row.Category)
			}
			seenCategories[row.Category] = true
			bottom := row.Y + rowHeight
			for index, placement := range row.Panels {
				if placement.X < 0 || placement.X >= GridColumns {
					t.Errorf("round %d: panel %d X = %d outside [0, 24)", round, placement.ID, placement.X)
				}
				if placement.W < 1 || placement.W > GridColumns {
					t.Errorf("round %d: panel %d W = %d outside [1, 24]", round, placement.ID, placement.W)
				}
				if placement.H < 1 || placement.H > GridColumns {
					t.Errorf("round %d: panel %d H = %d outside [1, 24]", round, placement.ID, placement.H)
				}
				if placement.X+placement.W > GridColumns {
					t.Errorf("round %d: panel %d overflows the grid: X=%d W=%d", round, placement.ID, placement.X, placement.W)
				}
				if placement.Y < row.Y+rowHeight {
					t.Errorf("round %d: panel %d Y=%d above its row", round, placement.ID, placement.Y)
				}
				for other := 0; other < index; other++ {
					if overlap(placement, row.Panels[other]) {
						t.Errorf("round %d: panels %d and %d overlap", round, placement.ID, row.Panels[other].ID)
					}
				}
				if placement.Y+placement.H > bottom {
					bottom = placement.Y + placement.H
				}
			}
			previousBottom = bottom
		}
	}
}

func overlap(first, second GridPlacement) bool {
	return first.X < second.X+second.W &&
		second.X < first.X+first.W &&
		first.Y < second.Y+second.H &&
		second.Y < first.Y+first.H
}

// gridLCG is a private deterministic generator; layout tests never use
// the global RNG or the clock.
type gridLCG struct{ state uint64 }

func newGridLCG(seed uint64) *gridLCG { return &gridLCG{state: seed} }

func (g *gridLCG) next() uint64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return g.state
}

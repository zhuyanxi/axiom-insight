package dashboard

import "sort"

// Deterministic grid layout contract (P2-04, task 4). Layout is a pure
// function of the canonical panel set: fixed 24-column grid, fixed
// category order, category rows stacked top-to-bottom, panels placed in
// ascending ID order, empty categories omitted. Nothing depends on map
// order, input order or runtime state.
const (
	// GridColumns is the fixed Grafana grid width.
	GridColumns = 24
	// PanelWidthStat is the fixed width of overview stat panels.
	PanelWidthStat = 6
	// PanelWidthTimeSeries is the fixed width of time-series panels.
	PanelWidthTimeSeries = 12
	// PanelWidthTable is the fixed width of table panels.
	PanelWidthTable = 24
	// rowHeight is the height of a category row panel.
	rowHeight = 1
)

// GridPanel is one panel entering the layout. Width and Height are clamped
// to the legal [1, GridColumns] range so the layout stays total.
type GridPanel struct {
	// ID is the resolved panel ID.
	ID int64
	// Width is the panel width in columns (6, 12 or 24 for v1 types).
	Width int
	// Height is the panel height in rows.
	Height int
}

// GridPlacement is one positioned panel.
type GridPlacement struct {
	// ID is the resolved panel ID.
	ID int64
	// X and Y are the top-left grid coordinates.
	X, Y int
	// W and H are the panel extent.
	W, H int
}

// CategoryLayout is the per-category panel set entering PlanGrid.
type CategoryLayout struct {
	// Category selects the fixed layout slot.
	Category Category
	// RowID is the resolved row panel ID.
	RowID int64
	// Panels are the category panels in canonical order.
	Panels []GridPanel
}

// GridRow is one laid-out category row, including the implicit row panel
// at (0, Y) with size (GridColumns, rowHeight).
type GridRow struct {
	// RowID is the resolved row panel ID.
	RowID int64
	// Category is the row's category.
	Category Category
	// Y is the row panel's vertical position; the row panel itself spans
	// (0, Y, GridColumns, rowHeight).
	Y int
	// Panels are the placed category panels below the row.
	Panels []GridPlacement
}

// PlanGrid lays out categories in CategoryOrder, top to bottom. The row
// panel of a category occupies one row; its panels are placed in
// ascending ID order beneath it, wrapping at GridColumns. A category
// with no panels is omitted entirely (AC3): it contributes no row, no
// panels and no grid holes. Categories outside CategoryOrder are
// skipped. Every placement satisfies 0 <= X, X+W <= GridColumns,
// 1 <= W <= GridColumns, and no two panels within a category overlap.
func PlanGrid(categories []CategoryLayout) []GridRow {
	byCategory := make(map[Category]CategoryLayout, len(categories))
	for _, category := range categories {
		byCategory[category.Category] = category
	}

	var rows []GridRow
	currentY := 0
	for _, category := range CategoryOrder {
		layout, present := byCategory[category]
		if !present || len(layout.Panels) == 0 {
			continue
		}

		row := GridRow{
			RowID:    layout.RowID,
			Category: layout.Category,
			Y:        currentY,
		}
		lineY, lineHeight, cursorX := 0, 0, 0
		// Panels are placed in ascending ID order (sort is the only
		// allowed logarithmic step), so the geometry never depends on the
		// caller's panel order.
		ordered := append([]GridPanel(nil), layout.Panels...)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
		for _, panel := range ordered {
			width := clampGrid(panel.Width)
			height := clampGrid(panel.Height)
			if cursorX+width > GridColumns {
				cursorX = 0
				lineY += lineHeight
				lineHeight = 0
			}
			row.Panels = append(row.Panels, GridPlacement{
				ID: panel.ID,
				X:  cursorX, Y: currentY + rowHeight + lineY,
				W: width, H: height,
			})
			cursorX += width
			if height > lineHeight {
				lineHeight = height
			}
		}

		rows = append(rows, row)
		currentY += rowHeight + lineY + lineHeight
	}
	return rows
}

// clampGrid bounds a grid extent into the legal [1, GridColumns] range.
func clampGrid(extent int) int {
	if extent < 1 {
		return 1
	}
	if extent > GridColumns {
		return GridColumns
	}
	return extent
}

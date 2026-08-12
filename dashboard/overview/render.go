package overview

import (
	"fmt"
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// Render converts a validated overview plan into the P2-02 model:
// the datasource variable, the optional operation variable and the
// overview panels with deterministic IDs, refIds and grid positions.
// Grid positions use the P2-04 rules (fixed 24-column grid, stat width 6,
// table width 24, panels placed in ascending ID order); the row offset is
// added by the dashboard renderer.
func Render(plan *Plan) ([]model.Variable, []model.Panel, error) {
	if plan == nil {
		return nil, nil, fmt.Errorf("overview: render: plan is nil")
	}
	variables := make([]model.Variable, 0, 2)
	variables = append(variables, plan.DatasourceVariable)
	if plan.OperationVariable != nil {
		variables = append(variables, *plan.OperationVariable)
	}

	keys := make([]string, 0, len(plan.Panels))
	for _, panel := range plan.Panels {
		keys = append(keys, panel.Key)
	}
	ids := dashboard.ResolvePanelIDs(keys)

	panels := make([]model.Panel, 0, len(plan.Panels))
	for index, panel := range plan.Panels {
		targets, err := renderTargets(panel)
		if err != nil {
			return nil, nil, err
		}
		panels = append(panels, model.Panel{
			ID:          int(ids[index]),
			Title:       panel.Title,
			Description: panel.Description,
			Type:        panel.Type,
			Datasource: &model.DatasourceRef{
				Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable,
			},
			Targets: targets,
			FieldConfig: model.FieldConfig{
				Defaults: model.FieldConfigDefaults{Unit: panel.Unit, NoValue: panel.NoValue},
			},
		})
	}
	placePanels(panels, plan.Panels)
	return variables, panels, nil
}

func renderTargets(panel Panel) ([]model.Target, error) {
	keys := make([]string, 0, len(panel.Targets))
	for _, target := range panel.Targets {
		keys = append(keys, target.CanonicalKey)
	}
	refIDs, err := dashboard.AllocateRefIDs(keys)
	if err != nil {
		return nil, fmt.Errorf("overview: render: panel %s: %w", panel.Purpose, err)
	}
	targets := make([]model.Target, 0, len(panel.Targets))
	for index, target := range panel.Targets {
		categories := make([]string, 0, len(target.Categories))
		for _, category := range target.Categories {
			categories = append(categories, string(category))
		}
		targets = append(targets, model.Target{
			RefID: refIDs[index],
			Datasource: &model.DatasourceRef{
				Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable,
			},
			Expr:         target.Expr,
			Format:       "time_series",
			LegendFormat: target.LegendFormat,
			Metadata: &model.QueryMetadata{
				Kind: target.Kind, Categories: categories,
				ItemIDs: append([]string(nil), target.ItemIDs...),
				PlanIDs: append([]string(nil), target.PlanIDs...),
			},
		})
	}
	return targets, nil
}

// placePanels assigns grid positions with the P2-04 packing rules: panels
// in ascending ID order, wrapping at 24 columns, each line advancing by
// its tallest panel. The plan panels supply the extents.
func placePanels(panels []model.Panel, plan []Panel) {
	ordered := make([]int, 0, len(panels))
	for index := range panels {
		ordered = append(ordered, index)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return panels[ordered[left]].ID < panels[ordered[right]].ID
	})
	positions := make(map[int]model.GridPos, len(panels))
	cursorX, lineY, lineHeight := 0, 0, 0
	for _, panelIndex := range ordered {
		width := clampGrid(plan[panelIndex].Width)
		height := clampGrid(plan[panelIndex].Height)
		if cursorX+width > dashboard.GridColumns {
			cursorX = 0
			lineY += lineHeight
			lineHeight = 0
		}
		positions[panels[panelIndex].ID] = model.GridPos{X: cursorX, Y: lineY, W: width, H: height}
		cursorX += width
		if height > lineHeight {
			lineHeight = height
		}
	}
	for index := range panels {
		panels[index].GridPos = positions[panels[index].ID]
	}
}

func clampGrid(extent int) int {
	if extent < 1 {
		return 1
	}
	if extent > dashboard.GridColumns {
		return dashboard.GridColumns
	}
	return extent
}

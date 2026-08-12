package category

import (
	"fmt"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// Render converts a validated category plan into P2-02 rows: one row per
// category with panels, deterministic IDs, refIds and grid positions.
// Rows without panels are omitted (no empty rows). Grid positions follow
// the P2-04 rules relative to the row content.
func Render(plan *Plan) ([]model.Row, error) {
	if plan == nil {
		return nil, fmt.Errorf("category: render: plan is nil")
	}
	keys := make([]string, 0, len(plan.Rows))
	for _, row := range plan.Rows {
		keys = append(keys, row.RowKey)
	}
	ids := dashboard.ResolvePanelIDs(keys)

	rows := make([]model.Row, 0, len(plan.Rows))
	for index, rowPlan := range plan.Rows {
		if len(rowPlan.Panels) == 0 {
			continue
		}
		row := model.Row{
			ID:          int(ids[index]),
			Title:       rowPlan.Title,
			Description: rowPlan.Description,
		}
		panelKeys := make([]string, 0, len(rowPlan.Panels))
		for _, panel := range rowPlan.Panels {
			panelKeys = append(panelKeys, panel.Key)
		}
		panelIDs := dashboard.ResolvePanelIDs(panelKeys)
		for panelIndex, panel := range rowPlan.Panels {
			targets, err := renderTargets(panel)
			if err != nil {
				return nil, err
			}
			row.Panels = append(row.Panels, model.Panel{
				ID:          int(panelIDs[panelIndex]),
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
				Links: panel.Links,
			})
		}
		if err := placePanels(row.Panels, rowPlan.Panels, rowPlan.Category); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func renderTargets(panel Panel) ([]model.Target, error) {
	keys := make([]string, 0, len(panel.Targets))
	for _, target := range panel.Targets {
		keys = append(keys, target.CanonicalKey)
	}
	refIDs, err := dashboard.AllocateRefIDs(keys)
	if err != nil {
		return nil, fmt.Errorf("category: render: panel %s: %w", panel.Purpose, err)
	}
	targets := make([]model.Target, 0, len(panel.Targets))
	for index, target := range panel.Targets {
		targets = append(targets, model.Target{
			RefID: refIDs[index],
			Datasource: &model.DatasourceRef{
				Type: model.DatasourceTypePrometheus, UID: model.DatasourceVariable,
			},
			Expr:         target.Expr,
			Format:       "time_series",
			LegendFormat: target.LegendFormat,
			Metadata: &model.QueryMetadata{
				PlanID: target.PlanID, TargetID: target.TargetID, Kind: target.Kind,
			},
		})
	}
	return targets, nil
}

// placePanels assigns grid positions with the P2-04 packing rules and
// offsets the row panel line so nested positions start at y=0.
func placePanels(panels []model.Panel, plan []Panel, category dashboard.Category) error {
	if len(panels) == 0 {
		return nil
	}
	gridPanels := make([]dashboard.GridPanel, 0, len(panels))
	for index := range panels {
		gridPanels = append(gridPanels, dashboard.GridPanel{
			ID: int64(panels[index].ID), Width: plan[index].Width, Height: plan[index].Height,
		})
	}
	rows := dashboard.PlanGrid([]dashboard.CategoryLayout{{
		Category: category, RowID: int64(panels[0].ID), Panels: gridPanels,
	}})
	if len(rows) != 1 {
		return fmt.Errorf("category: render: layout produced %d rows, want 1", len(rows))
	}
	for _, placement := range rows[0].Panels {
		for index := range panels {
			if panels[index].ID == int(placement.ID) {
				panels[index].GridPos = model.GridPos{
					X: placement.X, Y: placement.Y - 1, W: placement.W, H: placement.H,
				}
			}
		}
	}
	return nil
}

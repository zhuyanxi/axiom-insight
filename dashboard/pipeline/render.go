package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
)

// Result is one rendered dashboard: the canonical JSON bytes plus the
// definition summary consumed by the P2-11/P2-12 CLI report. The bytes
// are validated twice (before and after rendering) before they leave
// this boundary.
type Result struct {
	// Bytes are the canonical dashboard JSON (two-space indentation,
	// trailing LF, no HTML escaping, no timestamps).
	Bytes []byte
	// SHA256 is the hex SHA-256 of Bytes.
	SHA256 string
	// RowCount is the number of non-empty rows.
	RowCount int
	// PanelCount is the number of non-row panels.
	PanelCount int
	// QueryCount is the number of query targets.
	QueryCount int
}

// Render validates the typed plan model, renders canonical JSON,
// strictly decodes and re-validates the rendered bytes, then computes
// the hash and definition counts. Any failure returns
// DASHBOARD_RENDER_ERROR and no partial result.
func Render(plan *Plan) (*Result, error) {
	if plan == nil {
		return nil, &dashboard.CatalogError{
			Code: dashboard.CodeInvalidIR, Field: "plan", Message: "plan is nil",
		}
	}
	renderFailure := func(field, message string) error {
		return &dashboard.CatalogError{
			Code: dashboard.CodeRenderError, Field: field, Message: message,
		}
	}

	dashboard := plan.toModel()
	if violations := model.Validate(dashboard); len(violations) > 0 {
		return nil, renderFailure("model", fmt.Sprintf("dashboard failed semantic validation: %v", violations[0]))
	}
	bytes, err := model.Render(dashboard)
	if err != nil {
		return nil, renderFailure("render", err.Error())
	}
	// Post-render strict validation: the bytes must decode back into an
	// equivalent valid model (P2-10 task 3).
	decoded, err := model.Decode(bytes)
	if err != nil {
		return nil, renderFailure("render", fmt.Sprintf("rendered JSON failed strict decode: %v", err))
	}
	if violations := model.Validate(decoded); len(violations) > 0 {
		return nil, renderFailure("render", fmt.Sprintf("rendered JSON failed re-validation: %v", violations[0]))
	}

	sum := sha256.Sum256(bytes)
	return &Result{
		Bytes:      bytes,
		SHA256:     hex.EncodeToString(sum[:]),
		RowCount:   len(plan.rows),
		PanelCount: totalPanels(plan.rows),
		QueryCount: totalQueries(plan.rows),
	}, nil
}

// toModel converts the immutable plan into the typed P2-02 model. Rows
// and top-level panels are mutually exclusive in the contract, so the
// overview always travels inside its own row and the top-level panel
// list stays empty.
func (plan *Plan) toModel() *model.Dashboard {
	return &model.Dashboard{
		SchemaVersion: model.SchemaVersion,
		Title:         plan.title,
		UID:           plan.uid,
		ID:            nil,
		Version:       0,
		Editable:      true,
		Timezone:      plan.timezone,
		Refresh:       plan.refresh,
		Templating:    model.Templating{List: plan.variables},
		Rows:          plan.rows,
		Annotations:   model.Annotations{},
	}
}

package model

import (
	"fmt"
	"strings"
)

// ValidationError describes one semantic violation. Field is a dotted
// JSON path; ID locates the offending object.
type ValidationError struct {
	Field   string
	ID      string
	Message string
}

// Error implements error.
func (violation *ValidationError) Error() string {
	if violation.ID != "" {
		return fmt.Sprintf("%s %s: %s", violation.Field, violation.ID, violation.Message)
	}
	return fmt.Sprintf("%s: %s", violation.Field, violation.Message)
}

// Validate runs the semantic checks over a decoded dashboard. A nil
// result means valid; the input model is never modified.
func Validate(dashboard *Dashboard) []*ValidationError {
	if dashboard == nil {
		return []*ValidationError{{Field: "$", Message: "dashboard is nil"}}
	}
	var violations []*ValidationError
	emit := func(field, id, message string) {
		violations = append(violations, &ValidationError{Field: field, ID: id, Message: message})
	}

	if dashboard.SchemaVersion != SchemaVersion {
		emit("schemaVersion", "", fmt.Sprintf("must be %d (Grafana Schema 41)", SchemaVersion))
	}
	if dashboard.ID != nil {
		emit("id", "", "must be null; Grafana server IDs are forbidden")
	}
	if dashboard.Version != 0 {
		emit("version", "", "must be 0")
	}
	if !dashboard.Editable {
		emit("editable", "", "must be true")
	}
	if dashboard.Title == "" {
		emit("title", "", "title is empty")
	}
	if len(dashboard.Title) > MaxPanelTitleLength {
		emit("title", "", "title is too long")
	}
	if dashboard.UID != "" {
		if !validUID(dashboard.UID) {
			emit("uid", dashboard.UID, "invalid UID charset or length")
		}
	}
	if dashboard.Timezone != "" && dashboard.Timezone != "browser" && dashboard.Timezone != "utc" {
		emit("timezone", dashboard.Timezone, "must be browser or utc")
	}
	if len(dashboard.Rows) > 0 && len(dashboard.Panels) > 0 {
		emit("panels", "", "rows and top-level panels are mutually exclusive")
	}

	// The reserved datasource variable must exist whenever the
	// controlled reference is used.
	hasDatasourceVariable := false
	for _, variable := range dashboard.Templating.List {
		if variable.Name == "datasource" {
			hasDatasourceVariable = true
		}
	}

	panelIDs := make(map[int]bool)
	rowIDs := make(map[int]bool)
	for rowIndex, row := range dashboard.Rows {
		rowField := fmt.Sprintf("rows[%d]", rowIndex)
		if row.ID <= 0 {
			emit(rowField+".id", itoa(row.ID), "row ID must be positive")
		} else if rowIDs[row.ID] {
			emit(rowField+".id", itoa(row.ID), "duplicate row ID")
		} else {
			rowIDs[row.ID] = true
		}
		for panelIndex, panel := range row.Panels {
			validatePanel(&violations, emit, panel, fmt.Sprintf("%s.panels[%d]", rowField, panelIndex), panelIDs, hasDatasourceVariable)
		}
	}
	for panelIndex, panel := range dashboard.Panels {
		validatePanel(&violations, emit, panel, fmt.Sprintf("panels[%d]", panelIndex), panelIDs, hasDatasourceVariable)
	}

	variableNames := make(map[string]bool)
	for variableIndex, variable := range dashboard.Templating.List {
		variableField := fmt.Sprintf("templating.list[%d]", variableIndex)
		if variable.Name == "" {
			emit(variableField+".name", "", "variable name is empty")
		} else if variableNames[variable.Name] {
			emit(variableField+".name", variable.Name, "duplicate variable name")
		} else {
			variableNames[variable.Name] = true
		}
		switch variable.Type {
		case VariableTypeQuery:
			if variable.Query == "" {
				emit(variableField+".query", variable.Name, "query variable requires a query")
			} else if !validSelector(variable.Query) {
				emit(variableField+".query", variable.Name, "query must be a controlled metric selector")
			}
		case VariableTypeCustom, VariableTypeDatasource:
		default:
			emit(variableField+".type", variable.Name, "unsupported variable type")
		}
		if variable.Hide != nil && (*variable.Hide < 0 || *variable.Hide > 2) {
			emit(variableField+".hide", variable.Name, "hide must be 0, 1 or 2")
		}
		if variable.Type == VariableTypeQuery || variable.Type == VariableTypeDatasource {
			if variable.Datasource != nil {
				validateDatasource(&violations, emit, variable.Datasource, variableField+".datasource")
			}
		}
	}

	for rowIndex, row := range dashboard.Rows {
		for panelIndex, panel := range row.Panels {
			if len(panel.FieldConfig.Defaults.NoValue) > MaxNoValueLength {
				emit(fmt.Sprintf("rows[%d].panels[%d].fieldConfig.defaults.noValue", rowIndex, panelIndex), itoa(panel.ID), "no-value text is too long")
			}
		}
	}
	for panelIndex, panel := range dashboard.Panels {
		if len(panel.FieldConfig.Defaults.NoValue) > MaxNoValueLength {
			emit(fmt.Sprintf("panels[%d].fieldConfig.defaults.noValue", panelIndex), itoa(panel.ID), "no-value text is too long")
		}
	}

	for linkIndex, link := range dashboard.Links {
		linkField := fmt.Sprintf("links[%d]", linkIndex)
		if link.Title == "" {
			emit(linkField+".title", "", "link title is empty")
		}
		if !validLinkURL(link.URL) {
			emit(linkField+".url", link.URL, "link URL must be relative or internal; external URLs are rejected")
		}
	}

	for annotationIndex, annotation := range dashboard.Annotations.List {
		annotationField := fmt.Sprintf("annotations.list[%d]", annotationIndex)
		if annotation.Name == "" {
			emit(annotationField+".name", "", "annotation name is empty")
		}
		if annotation.Datasource != nil {
			validateDatasource(&violations, emit, annotation.Datasource, annotationField+".datasource")
		}
		if annotation.Target != nil {
			validateTarget(&violations, emit, *annotation.Target, annotationField+".target", hasDatasourceVariable)
		}
	}

	return violations
}

func validatePanel(violations *[]*ValidationError, emit func(string, string, string), panel Panel, field string, panelIDs map[int]bool, hasDatasourceVariable bool) {
	if panel.ID <= 0 {
		emit(field+".id", itoa(panel.ID), "panel ID must be positive")
	} else if panelIDs[panel.ID] {
		emit(field+".id", itoa(panel.ID), "duplicate panel ID")
	} else {
		panelIDs[panel.ID] = true
	}
	if panel.Title == "" {
		emit(field+".title", "", "panel title is empty")
	}
	if len(panel.Title) > MaxPanelTitleLength {
		emit(field+".title", "", "panel title is too long")
	}
	if len(panel.Description) > MaxDescriptionLength {
		emit(field+".description", itoa(panel.ID), "panel description is too long")
	}
	if !oneOf(panel.Type, PanelTypeTimeSeries, PanelTypeStat, PanelTypeGauge, PanelTypeTable, PanelTypeRow) {
		emit(field+".type", panel.Type, "unsupported panel type")
	}
	grid := panel.GridPos
	if grid.X < 0 || grid.Y < 0 || grid.W < 1 || grid.H < 1 || grid.W > 24 || grid.H > 24 || grid.X+grid.W > 24 {
		emit(field+".gridPos", itoa(panel.ID), "grid position out of bounds")
	}
	if panel.Type != PanelTypeRow {
		if panel.Datasource != nil {
			validateDatasource(violations, emit, panel.Datasource, field+".datasource")
			if !hasDatasourceVariable && panel.Datasource.UID == DatasourceVariable {
				emit(field+".datasource", itoa(panel.ID), "reserved datasource variable is not declared in templating")
			}
		}
		if len(panel.Targets) > MaxTargetsPerPanel {
			emit(field+".targets", itoa(panel.ID), "panel exceeds the target limit")
		}
		refIDs := make(map[string]bool, len(panel.Targets))
		for targetIndex, target := range panel.Targets {
			validateTarget(violations, emit, target, fmt.Sprintf("%s.targets[%d]", field, targetIndex), hasDatasourceVariable)
			if len(target.RefID) == 1 && target.RefID[0] >= 'A' && target.RefID[0] <= 'Z' {
				if refIDs[target.RefID] {
					emit(fmt.Sprintf("%s.targets[%d].refId", field, targetIndex), target.RefID, "duplicate refId")
				} else {
					refIDs[target.RefID] = true
				}
			} else {
				emit(fmt.Sprintf("%s.targets[%d].refId", field, targetIndex), target.RefID, "refId must be a single letter A..Z")
			}
		}
	}
	for linkIndex, link := range panel.Links {
		linkField := fmt.Sprintf("%s.links[%d]", field, linkIndex)
		if !validLinkURL(link.URL) {
			emit(linkField+".url", link.URL, "link URL must be relative or internal; external URLs are rejected")
		}
	}
}

func validateTarget(violations *[]*ValidationError, emit func(string, string, string), target Target, field string, hasDatasourceVariable bool) {
	if target.Expr == "" {
		emit(field+".expr", target.RefID, "target expression is empty")
	}
	if target.Format != "time_series" {
		emit(field+".format", target.RefID, "format must be time_series")
	}
	if target.Datasource != nil {
		validateDatasource(violations, emit, target.Datasource, field+".datasource")
		if !hasDatasourceVariable && target.Datasource.UID == DatasourceVariable {
			emit(field+".datasource", target.RefID, "reserved datasource variable is not declared in templating")
		}
	}
	if target.Metadata != nil {
		validateQueryMetadata(violations, emit, target.Metadata, field+".metadata", target.RefID)
	}
}

func validateQueryMetadata(violations *[]*ValidationError, emit func(string, string, string), metadata *QueryMetadata, field, refID string) {
	if metadata.Kind == "" {
		emit(field+".kind", refID, "query metadata must carry kind")
	}
	legacy := metadata.PlanID != "" && metadata.TargetID != ""
	halfLegacy := (metadata.PlanID == "") != (metadata.TargetID == "")
	overview := len(metadata.Categories) > 0 && len(metadata.ItemIDs) > 0 && len(metadata.PlanIDs) > 0
	if halfLegacy {
		emit(field, refID, "query metadata must set plan_id and target_id together")
	}
	if legacy && overview {
		emit(field, refID, "query metadata must use exactly one of plan_id+target_id or categories+item_ids+plan_ids")
	}
	if !legacy && !overview {
		emit(field, refID, "query metadata must carry plan_id+target_id or categories+item_ids+plan_ids")
	}
	if overview {
		for _, category := range metadata.Categories {
			if category == "" {
				emit(field+".categories", refID, "category references must not be empty")
			}
		}
		for _, itemID := range metadata.ItemIDs {
			if itemID == "" {
				emit(field+".item_ids", refID, "item references must not be empty")
			}
		}
		for _, planID := range metadata.PlanIDs {
			if planID == "" {
				emit(field+".plan_ids", refID, "plan references must not be empty")
			}
		}
	}
}

func validateDatasource(violations *[]*ValidationError, emit func(string, string, string), datasource *DatasourceRef, field string) {
	if !oneOf(datasource.Type, DatasourceTypePrometheus, DatasourceTypeTempo) {
		emit(field+".type", datasource.Type, "unsupported datasource type")
	}
	if datasource.UID != DatasourceVariable {
		emit(field+".uid", datasource.UID, "datasource must be the reserved "+DatasourceVariable+" variable")
	}
	if strings.Contains(datasource.UID, "://") {
		emit(field+".uid", datasource.UID, "datasource must not carry an endpoint or credentials")
	}
}

func validUID(uid string) bool {
	if len(uid) == 0 || len(uid) > MaxUIDLength {
		return false
	}
	for _, character := range uid {
		if !(character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_') {
			return false
		}
	}
	return true
}

// validSelector accepts controlled metric selector expressions:
// metric names, brace label selectors and the rate/quantile wrappers
// produced by the Phase 2 query planner. label_values, user regexes and
// raw values are rejected.
func validSelector(expression string) bool {
	if len(expression) == 0 || len(expression) > 512 {
		return false
	}
	if strings.Contains(expression, "label_values") || strings.Contains(expression, "=~") {
		return false
	}
	for _, character := range expression {
		if !(character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("_${}()=.,: \"*-", character)) {
			return false
		}
	}
	return true
}

// validLinkURL accepts relative and internal /d/ links only.
func validLinkURL(url string) bool {
	if url == "" {
		return false
	}
	if strings.Contains(url, "://") || strings.HasPrefix(url, "http") {
		return false
	}
	if strings.ContainsAny(url, " \t\n") {
		return false
	}
	return true
}

func oneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [32]byte
	position := len(digits)
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		position--
		digits[position] = '-'
	}
	return string(digits[position:])
}

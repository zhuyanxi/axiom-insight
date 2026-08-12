package model

// Contract constants (closed v1).
const (
	// SchemaVersion is the pinned Grafana schema version.
	SchemaVersion = 41
	// ContractVersion is the dashboard contract version string.
	ContractVersion = "grafana.dashboard/v1"
	// DatasourceVariable is the reserved, controlled datasource variable
	// reference.
	DatasourceVariable = "${datasource}"
	// MaxDocumentBytes bounds a dashboard document.
	MaxDocumentBytes = 10 << 20
	// MaxDepth bounds JSON nesting.
	MaxDepth = 64
	// MaxTargetsPerPanel bounds panel targets (A..Z).
	MaxTargetsPerPanel = 26
	// MaxPanelTitleLength bounds panel and row titles.
	MaxPanelTitleLength = 255
	// MaxDescriptionLength bounds panel descriptions.
	MaxDescriptionLength = 255
	// MaxNoValueLength bounds the field-config no-value text.
	MaxNoValueLength = 64
	// MaxUIDLength bounds dashboard UIDs.
	MaxUIDLength = 40
)

// Panel types.
const (
	PanelTypeTimeSeries = "timeseries"
	PanelTypeStat       = "stat"
	PanelTypeGauge      = "gauge"
	PanelTypeTable      = "table"
	PanelTypeRow        = "row"
)

// Datasource types.
const (
	DatasourceTypePrometheus = "prometheus"
	DatasourceTypeTempo      = "tempo"
)

// Variable types.
const (
	VariableTypeQuery      = "query"
	VariableTypeCustom     = "custom"
	VariableTypeDatasource = "datasource"
)

// Dashboard is the typed top-level model. It is the closed
// grafana.dashboard/v1 contract; no map[string]any is used at the top
// level or inside panels.
type Dashboard struct {
	// SchemaVersion is fixed to 41 by the contract.
	SchemaVersion int `json:"schemaVersion"`
	// Title is the dashboard title.
	Title string `json:"title"`
	// UID is the deterministic dashboard UID.
	UID string `json:"uid,omitempty"`
	// ID is fixed to null; Grafana server IDs are forbidden.
	ID any `json:"id"`
	// Version is fixed to 0.
	Version int `json:"version"`
	// Editable is fixed to true.
	Editable bool `json:"editable"`
	// Tags are controlled static tags.
	Tags []string `json:"tags,omitempty"`
	// Timezone is "browser" or "utc".
	Timezone string `json:"timezone,omitempty"`
	// Refresh is a controlled refresh interval.
	Refresh string `json:"refresh,omitempty"`
	// Templating holds the controlled variables.
	Templating Templating `json:"templating"`
	// Rows hold the row panels with their nested panels.
	Rows []Row `json:"rows"`
	// Panels holds top-level (non-row) panels; rows and panels are
	// mutually exclusive.
	Panels []Panel `json:"panels,omitempty"`
	// Links are controlled dashboard links.
	Links []Link `json:"links,omitempty"`
	// Annotations are the controlled annotations.
	Annotations Annotations `json:"annotations"`
}

// Templating holds the controlled variable list.
type Templating struct {
	List []Variable `json:"list,omitempty"`
}

// Variable is one controlled dashboard variable.
type Variable struct {
	// Name is the variable name; "datasource" is reserved.
	Name string `json:"name"`
	// Label is the display label.
	Label string `json:"label,omitempty"`
	// Type is query, custom or datasource.
	Type string `json:"type"`
	// Hide is the Grafana variable visibility: 0 visible, 1 label-only
	// hide, 2 full hide. The pointer keeps "explicitly 0" distinct from
	// "absent" so the datasource variable serializes hide 0.
	Hide *int `json:"hide,omitempty"`
	// Datasource is the controlled datasource reference for query
	// variables.
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	// Query is the controlled variable query (a metric selector, never
	// label_values or user regexes).
	Query string `json:"query,omitempty"`
	// Options are static custom options.
	Options []VariableOption `json:"options,omitempty"`
	// Current is the current value expression.
	Current *VariableCurrent `json:"current,omitempty"`
	// Multi and IncludeAll are controlled flags.
	Multi      bool `json:"multi,omitempty"`
	IncludeAll bool `json:"includeAll,omitempty"`
}

// VariableOption is one static variable option.
type VariableOption struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

// VariableCurrent is the current value; values are scalars, never
// arbitrary maps.
type VariableCurrent struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

// Row is a dashboard row with its nested panels.
type Row struct {
	// ID is the deterministic row ID.
	ID int `json:"id"`
	// Title is the row title.
	Title string `json:"title"`
	// Panels are the panels nested in this row.
	Panels []Panel `json:"panels,omitempty"`
}

// Panel is one controlled panel.
type Panel struct {
	// ID is the deterministic panel ID, unique across the dashboard.
	ID int `json:"id"`
	// Title is the panel title.
	Title string `json:"title"`
	// Description explains the panel; it is static controlled text, never
	// user content.
	Description string `json:"description,omitempty"`
	// Type is timeseries, stat, gauge, table or row.
	Type string `json:"type"`
	// GridPos is the panel grid position.
	GridPos GridPos `json:"gridPos"`
	// Datasource is the controlled datasource reference.
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	// Targets are the controlled query targets (max 26).
	Targets []Target `json:"targets,omitempty"`
	// FieldConfig is the controlled field configuration.
	FieldConfig FieldConfig `json:"fieldConfig"`
	// Links are the controlled panel links.
	Links []Link `json:"links,omitempty"`
}

// GridPos is the panel grid position.
type GridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Target is one controlled query target.
type Target struct {
	// RefID is the controlled ref ID, A..Z, unique per panel.
	RefID string `json:"refId"`
	// Datasource is the controlled datasource reference.
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	// Expr is the controlled query expression (PromQL for prometheus).
	Expr string `json:"expr"`
	// Format is fixed to "time_series".
	Format string `json:"format"`
	// QueryType is the controlled query type.
	QueryType string `json:"queryType,omitempty"`
	// LegendFormat is the controlled legend format.
	LegendFormat string `json:"legendFormat,omitempty"`
	// Metadata carries the Phase 2 traceability to the Plan.
	Metadata *QueryMetadata `json:"metadata,omitempty"`
}

// QueryMetadata traces a target to its plan declaration.
type QueryMetadata struct {
	// PlanID is the GenerationPlan item ID the query is derived from.
	// Set for per-item queries (the legacy v1 form).
	PlanID string `json:"plan_id,omitempty"`
	// TargetID is the IR entity ID; set with PlanID for per-item queries.
	TargetID string `json:"target_id,omitempty"`
	// Kind is the query kind, e.g. "rate" or "percentile".
	Kind string `json:"kind"`
	// Categories are the overview category references of an aggregated
	// overview query, sorted and deduplicated.
	Categories []string `json:"categories,omitempty"`
	// ItemIDs are the catalog item references of an aggregated overview
	// query, sorted and deduplicated.
	ItemIDs []string `json:"item_ids,omitempty"`
	// PlanIDs are the GenerationPlan item IDs of an aggregated overview
	// query, sorted and deduplicated.
	PlanIDs []string `json:"plan_ids,omitempty"`
}

// DatasourceRef is the controlled datasource reference; only the
// reserved variable is allowed.
type DatasourceRef struct {
	// Type is prometheus or tempo.
	Type string `json:"type"`
	// UID is fixed to "${datasource}".
	UID string `json:"uid"`
}

// FieldConfig is the controlled field configuration.
type FieldConfig struct {
	Defaults  FieldConfigDefaults `json:"defaults,omitempty"`
	Overrides []FieldOverride     `json:"overrides,omitempty"`
}

// FieldConfigDefaults is the default field settings.
type FieldConfigDefaults struct {
	Unit    string   `json:"unit,omitempty"`
	NoValue string   `json:"noValue,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

// FieldOverride overrides one field by matcher.
type FieldOverride struct {
	Matcher    FieldMatcher    `json:"matcher"`
	Properties []FieldProperty `json:"properties,omitempty"`
}

// FieldMatcher selects the overridden field.
type FieldMatcher struct {
	ID string `json:"id"`
}

// FieldProperty sets one scalar property.
type FieldProperty struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

// Link is a controlled dashboard or panel link.
type Link struct {
	Title string `json:"title"`
	// URL is a relative or /d/ internal link; external URLs are
	// rejected.
	URL string `json:"url"`
	// TargetBlank opens the link in a new tab.
	TargetBlank bool `json:"targetBlank,omitempty"`
}

// Annotations holds the controlled annotation list.
type Annotations struct {
	List []Annotation `json:"list,omitempty"`
}

// Annotation is one controlled annotation query.
type Annotation struct {
	Name       string         `json:"name"`
	Datasource *DatasourceRef `json:"datasource,omitempty"`
	Enable     bool           `json:"enable,omitempty"`
	Hide       bool           `json:"hide,omitempty"`
	IconColor  string         `json:"iconColor,omitempty"`
	Target     *Target        `json:"target,omitempty"`
}

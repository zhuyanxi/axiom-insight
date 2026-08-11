package dashboard

import "regexp"

// Built-in defaults for the si.yaml `dashboard` node (P2-03 contract,
// section 2.6). Values mirror the Phase 2 contract; changing a default
// requires an explicit review of the default-policy snapshot test.
const (
	// DefaultOutputDir is the default dashboard output directory relative
	// to the source root.
	DefaultOutputDir = "dashboards"
	// DefaultTitleSuffix is appended to the dashboard title.
	DefaultTitleSuffix = "Observability"
	// DefaultDatasourceVariableName is the controlled datasource variable;
	// it is reserved and cannot be removed. v1 allows no other value.
	DefaultDatasourceVariableName = "datasource"
	// DefaultRateInterval is the PromQL rate window; v1 allows no other
	// value.
	DefaultRateInterval = "$__rate_interval"
	// DefaultTimezone is the dashboard timezone.
	DefaultTimezone = "browser"
	// DefaultRefresh is the dashboard refresh interval.
	DefaultRefresh = "30s"
	// DefaultMaxPanels is the default upper bound on generated panels.
	DefaultMaxPanels = 200
	// HardMaxPanels is the implementation safety ceiling for max_panels;
	// user values above it are rejected.
	HardMaxPanels = 1000
	// DefaultMaxQueries is the default upper bound on generated queries.
	DefaultMaxQueries = 500
	// HardMaxQueries is the implementation safety ceiling for max_queries;
	// user values above it are rejected.
	HardMaxQueries = 5000
	// MaxTitleSuffixLength bounds the dashboard title suffix length.
	MaxTitleSuffixLength = 64
	// MaxConfigBytes is the si.yaml size ceiling the dashboard decoder
	// accepts.
	MaxConfigBytes = 1 << 20
)

// allowedTimezones is the v1 timezone allowlist.
var allowedTimezones = []string{"browser", "utc"}

// allowedRefreshes is the v1 refresh allowlist.
var allowedRefreshes = []string{"5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h", "off"}

// datasourceVariablePattern is the allowed datasource variable charset:
// ASCII letter, digit or underscore, starting with a letter or underscore,
// at most 32 characters.
var datasourceVariablePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,31}$`)

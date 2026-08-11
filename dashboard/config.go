package dashboard

import (
	"fmt"
	"slices"
	"strings"
)

// DashboardConfig is the user-supplied si.yaml `dashboard` node (P2-03).
// Every field that a default could override is a pointer so the merge
// rules can distinguish "explicitly set" from "absent": an explicit false
// survives the default layer and an explicit CLI value survives the YAML
// layer. A nil pointer always means "not set".
//
// Values are validated only after the full merge; Resolve is the only
// entry point that produces a valid, immutable DashboardPolicy.
type DashboardConfig struct {
	// OutputDir is the dashboard output directory relative to the source
	// root, or an explicit absolute directory.
	OutputDir *string `json:"output_dir"`
	// TitleSuffix is appended to the dashboard title.
	TitleSuffix *string `json:"title_suffix"`
	// DatasourceVariableName names the controlled datasource variable;
	// v1 allows only the reserved name "datasource".
	DatasourceVariableName *string `json:"datasource_variable_name"`
	// IncludeTraceLinks controls trace deep links on capable panels.
	IncludeTraceLinks *bool `json:"include_trace_links"`
	// IncludeClientDependencies admits HTTP/RPC client items into the
	// catalog when true.
	IncludeClientDependencies *bool `json:"include_client_dependencies"`
	// RateInterval is the PromQL rate window; v1 allows only
	// "$__rate_interval".
	RateInterval *string `json:"rate_interval"`
	// Timezone is the dashboard timezone; "browser" or "utc".
	Timezone *string `json:"timezone"`
	// Refresh is the dashboard refresh interval; an allowlisted duration
	// or "off".
	Refresh *string `json:"refresh"`
	// MaxPanels bounds generated panels; positive and at most
	// HardMaxPanels.
	MaxPanels *int64 `json:"max_panels"`
	// MaxQueries bounds generated queries; positive and at most
	// HardMaxQueries.
	MaxQueries *int64 `json:"max_queries"`
	// Strict promotes dashboard warning diagnostics to failures.
	Strict *bool `json:"strict"`
}

// Overrides carries explicit CLI flag values for the dashboard pipeline.
// Merge priority is fixed: CLI flags > si.yaml > built-in defaults. A nil
// pointer means the flag was not set and the YAML/default value stands.
// P2-03 covers the policy-shaped flags only; execution flags (--dry-run,
// --force, --format) are registered by P2-11.
type Overrides struct {
	// OutputDir overrides dashboard.output_dir.
	OutputDir *string `json:"output_dir"`
	// Strict overrides dashboard.strict.
	Strict *bool `json:"strict"`
}

// DashboardPolicy is the fully resolved, validated dashboard
// configuration. It is immutable after Resolve returns: it exposes no
// mutable slices or maps, and every value is a fresh copy of the input.
type DashboardPolicy struct {
	// OutputDir is the resolved dashboard output directory.
	OutputDir string
	// TitleSuffix is the resolved dashboard title suffix.
	TitleSuffix string
	// DatasourceVariableName is the validated datasource variable name.
	DatasourceVariableName string
	// IncludeTraceLinks controls trace deep links.
	IncludeTraceLinks bool
	// IncludeClientDependencies admits HTTP/RPC client items.
	IncludeClientDependencies bool
	// RateInterval is the validated PromQL rate window.
	RateInterval string
	// Timezone is the validated dashboard timezone.
	Timezone string
	// Refresh is the validated dashboard refresh interval.
	Refresh string
	// MaxPanels bounds generated panels.
	MaxPanels int64
	// MaxQueries bounds generated queries.
	MaxQueries int64
	// Strict promotes warning diagnostics to failures.
	Strict bool
}

// ConfigError describes one configuration violation. Field is a dotted
// path into the dashboard node, for example "dashboard.refresh" or
// "dashboard.max_panels".
type ConfigError struct {
	// Field is the configuration path of the offending value.
	Field string
	// Message explains the violated rule without echoing the rejected
	// value, which could carry a secret.
	Message string
}

// ConfigErrors aggregates every violation found while resolving a
// dashboard configuration. It implements error and prefixes each line with
// the stable DASHBOARD_INVALID_CONFIG message code.
type ConfigErrors struct {
	violations []ConfigError
}

// Error implements error. It never includes the full configuration
// contents, only field paths and rule messages.
func (failures *ConfigErrors) Error() string {
	var builder strings.Builder
	for index, violation := range failures.violations {
		if index > 0 {
			builder.WriteByte('\n')
		}
		fmt.Fprintf(&builder, "%s: %s: %s", CodeInvalidConfig, violation.Field, violation.Message)
	}
	return builder.String()
}

// Violations returns the individual violations. The returned slice must
// not be modified.
func (failures *ConfigErrors) Violations() []ConfigError { return failures.violations }

// Resolve merges defaults, the YAML configuration and the CLI overrides
// (CLI > YAML > defaults), validates the result and returns the immutable
// DashboardPolicy. A nil config and nil overrides produce the built-in
// default policy. On any violation it returns nil and a *ConfigErrors
// whose paths locate every offending field.
func Resolve(config *DashboardConfig, overrides *Overrides) (*DashboardPolicy, error) {
	effective := newEffective()

	// Layer 1: built-in defaults.
	applyDefaults(effective)
	// Layer 2: explicit si.yaml values.
	applyConfig(effective, config)
	// Layer 3: explicit CLI flags.
	applyOverrides(effective, overrides)

	if violations := validateEffective(effective); len(violations) > 0 {
		return nil, &ConfigErrors{violations: violations}
	}
	return effective.build(), nil
}

// effective is the merged working configuration: every pointer is
// non-nil before validation runs.
type effective struct {
	outputDir                 string
	titleSuffix               string
	datasourceVariableName    string
	includeTraceLinks         bool
	includeClientDependencies bool
	rateInterval              string
	timezone                  string
	refresh                   string
	maxPanels                 int64
	maxQueries                int64
	strict                    bool
}

func newEffective() *effective { return &effective{} }

func applyDefaults(target *effective) {
	target.outputDir = DefaultOutputDir
	target.titleSuffix = DefaultTitleSuffix
	target.datasourceVariableName = DefaultDatasourceVariableName
	target.includeTraceLinks = true
	target.includeClientDependencies = true
	target.rateInterval = DefaultRateInterval
	target.timezone = DefaultTimezone
	target.refresh = DefaultRefresh
	target.maxPanels = DefaultMaxPanels
	target.maxQueries = DefaultMaxQueries
	target.strict = false
}

func applyConfig(target *effective, config *DashboardConfig) {
	if config == nil {
		return
	}
	if config.OutputDir != nil {
		target.outputDir = *config.OutputDir
	}
	if config.TitleSuffix != nil {
		target.titleSuffix = *config.TitleSuffix
	}
	if config.DatasourceVariableName != nil {
		target.datasourceVariableName = *config.DatasourceVariableName
	}
	if config.IncludeTraceLinks != nil {
		target.includeTraceLinks = *config.IncludeTraceLinks
	}
	if config.IncludeClientDependencies != nil {
		target.includeClientDependencies = *config.IncludeClientDependencies
	}
	if config.RateInterval != nil {
		target.rateInterval = *config.RateInterval
	}
	if config.Timezone != nil {
		target.timezone = *config.Timezone
	}
	if config.Refresh != nil {
		target.refresh = *config.Refresh
	}
	if config.MaxPanels != nil {
		target.maxPanels = *config.MaxPanels
	}
	if config.MaxQueries != nil {
		target.maxQueries = *config.MaxQueries
	}
	if config.Strict != nil {
		target.strict = *config.Strict
	}
}

func applyOverrides(target *effective, overrides *Overrides) {
	if overrides == nil {
		return
	}
	if overrides.OutputDir != nil {
		target.outputDir = *overrides.OutputDir
	}
	if overrides.Strict != nil {
		target.strict = *overrides.Strict
	}
}

// validateEffective checks every rule. Violations carry exact field paths
// such as "dashboard.refresh" or "dashboard.max_panels"; messages never
// echo the rejected value.
func validateEffective(target *effective) []ConfigError {
	var violations []ConfigError

	if strings.TrimSpace(target.outputDir) == "" {
		violations = append(violations, ConfigError{
			Field:   "dashboard.output_dir",
			Message: "output_dir must not be empty",
		})
	}
	if strings.ContainsRune(target.outputDir, 0) {
		violations = append(violations, ConfigError{
			Field:   "dashboard.output_dir",
			Message: "output_dir must not contain NUL",
		})
	}

	if runeCount(target.titleSuffix) > MaxTitleSuffixLength {
		violations = append(violations, ConfigError{
			Field:   "dashboard.title_suffix",
			Message: fmt.Sprintf("title_suffix must be at most %d characters", MaxTitleSuffixLength),
		})
	}

	if !datasourceVariablePattern.MatchString(target.datasourceVariableName) {
		violations = append(violations, ConfigError{
			Field:   "dashboard.datasource_variable_name",
			Message: "datasource_variable_name must match [A-Za-z_][A-Za-z0-9_]{0,31}",
		})
	}
	if target.datasourceVariableName != DefaultDatasourceVariableName {
		violations = append(violations, ConfigError{
			Field:   "dashboard.datasource_variable_name",
			Message: fmt.Sprintf("v1 allows only the reserved datasource variable name %q", DefaultDatasourceVariableName),
		})
	}

	if target.rateInterval != DefaultRateInterval {
		violations = append(violations, ConfigError{
			Field:   "dashboard.rate_interval",
			Message: fmt.Sprintf("rate_interval must be %s", DefaultRateInterval),
		})
	}

	if !allowedValue(allowedTimezones, target.timezone) {
		violations = append(violations, ConfigError{
			Field:   "dashboard.timezone",
			Message: "timezone must be one of: browser, utc",
		})
	}

	if !allowedValue(allowedRefreshes, target.refresh) {
		violations = append(violations, ConfigError{
			Field:   "dashboard.refresh",
			Message: "refresh must be one of: 5s, 10s, 30s, 1m, 5m, 15m, 30m, 1h, off",
		})
	}

	violations = append(violations, validateLimit("dashboard.max_panels", target.maxPanels, HardMaxPanels)...)
	violations = append(violations, validateLimit("dashboard.max_queries", target.maxQueries, HardMaxQueries)...)

	return violations
}

func validateLimit(path string, value, hardCap int64) []ConfigError {
	if value < 1 {
		return []ConfigError{{Field: path, Message: "must be a positive integer"}}
	}
	if value > hardCap {
		return []ConfigError{{
			Field:   path,
			Message: fmt.Sprintf("must not exceed the hard safety ceiling of %d", hardCap),
		}}
	}
	return nil
}

func allowedValue(allowlist []string, value string) bool {
	return slices.Contains(allowlist, value)
}

func runeCount(value string) int {
	count := 0
	for range value {
		count++
	}
	return count
}

func (target *effective) build() *DashboardPolicy {
	return &DashboardPolicy{
		OutputDir:                 target.outputDir,
		TitleSuffix:               target.titleSuffix,
		DatasourceVariableName:    target.datasourceVariableName,
		IncludeTraceLinks:         target.includeTraceLinks,
		IncludeClientDependencies: target.includeClientDependencies,
		RateInterval:              target.rateInterval,
		Timezone:                  target.timezone,
		Refresh:                   target.refresh,
		MaxPanels:                 target.maxPanels,
		MaxQueries:                target.maxQueries,
		Strict:                    target.strict,
	}
}

package dashboard

import (
	"errors"
	"strings"
	"testing"
)

func str(value string) *string { return &value }
func boolp(value bool) *bool   { return &value }
func intp(value int64) *int64  { return &value }

// TestDefaultPolicy fixes the documented defaults (AC1): zero
// configuration produces the documented output, datasource, rate interval,
// refresh, limits and trace/client switches.
func TestDefaultPolicy(t *testing.T) {
	policy, err := Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve(nil, nil) failed: %v", err)
	}
	expect := map[string]any{
		"OutputDir":                 "dashboards",
		"TitleSuffix":               "Observability",
		"DatasourceVariableName":    "datasource",
		"IncludeTraceLinks":         true,
		"IncludeClientDependencies": true,
		"RateInterval":              "$__rate_interval",
		"Timezone":                  "browser",
		"Refresh":                   "30s",
		"MaxPanels":                 int64(200),
		"MaxQueries":                int64(500),
		"Strict":                    false,
	}
	for field, want := range expect {
		got := map[string]any{
			"OutputDir":                 policy.OutputDir,
			"TitleSuffix":               policy.TitleSuffix,
			"DatasourceVariableName":    policy.DatasourceVariableName,
			"IncludeTraceLinks":         policy.IncludeTraceLinks,
			"IncludeClientDependencies": policy.IncludeClientDependencies,
			"RateInterval":              policy.RateInterval,
			"Timezone":                  policy.Timezone,
			"Refresh":                   policy.Refresh,
			"MaxPanels":                 policy.MaxPanels,
			"MaxQueries":                policy.MaxQueries,
			"Strict":                    policy.Strict,
		}[field]
		if got != want {
			t.Errorf("%s = %v, want %v", field, got, want)
		}
	}
}

// TestMergePrecedence verifies CLI > YAML > defaults and that an explicit
// YAML false survives the default layer (AC2).
func TestMergePrecedence(t *testing.T) {
	// Defaults, YAML and CLI give different values for the same field.
	yamlConfig := &DashboardConfig{
		OutputDir:    str("from-yaml"),
		Refresh:      str("1m"),
		MaxPanels:    intp(50),
		MaxQueries:   intp(60),
		Timezone:     str("utc"),
		TitleSuffix:  str("YAML Suffix"),
		RateInterval: str("$__rate_interval"),
	}
	overrides := &Overrides{
		OutputDir: str("from-cli"),
		Strict:    boolp(true),
	}
	policy, err := Resolve(yamlConfig, overrides)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if policy.OutputDir != "from-cli" {
		t.Errorf("CLI must win over YAML: OutputDir = %q", policy.OutputDir)
	}
	if policy.Refresh != "1m" || policy.MaxPanels != 50 || policy.MaxQueries != 60 {
		t.Errorf("YAML values must win over defaults: Refresh=%q MaxPanels=%d MaxQueries=%d",
			policy.Refresh, policy.MaxPanels, policy.MaxQueries)
	}
	if policy.Strict != true {
		t.Errorf("CLI strict must be true, got %v", policy.Strict)
	}
}

// TestExplicitFalseSurvives verifies an explicit YAML false is not
// overridden by the default layer.
func TestExplicitFalseSurvives(t *testing.T) {
	config := &DashboardConfig{
		IncludeTraceLinks:         boolp(false),
		IncludeClientDependencies: boolp(false),
		Strict:                    boolp(false),
	}
	policy, err := Resolve(config, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if policy.IncludeTraceLinks {
		t.Error("explicit include_trace_links=false was overridden by default")
	}
	if policy.IncludeClientDependencies {
		t.Error("explicit include_client_dependencies=false was overridden by default")
	}
	if policy.Strict {
		t.Error("explicit strict=false was overridden by default")
	}
}

// TestInvalidConfigurations rejects every rule violation with the exact
// field path and the DASHBOARD_INVALID_CONFIG code (AC3).
func TestInvalidConfigurations(t *testing.T) {
	longSuffix := strings.Repeat("s", MaxTitleSuffixLength+1)
	cases := []struct {
		name   string
		config *DashboardConfig
		field  string
	}{
		{name: "bad refresh", config: &DashboardConfig{Refresh: str("45s")}, field: "dashboard.refresh"},
		{name: "arbitrary rate interval", config: &DashboardConfig{RateInterval: str("$__interval")}, field: "dashboard.rate_interval"},
		{name: "variable injection", config: &DashboardConfig{DatasourceVariableName: str("$(ds)")}, field: "dashboard.datasource_variable_name"},
		{name: "non-reserved variable", config: &DashboardConfig{DatasourceVariableName: str("my_ds")}, field: "dashboard.datasource_variable_name"},
		{name: "zero max panels", config: &DashboardConfig{MaxPanels: intp(0)}, field: "dashboard.max_panels"},
		{name: "over hard cap panels", config: &DashboardConfig{MaxPanels: intp(HardMaxPanels + 1)}, field: "dashboard.max_panels"},
		{name: "zero max queries", config: &DashboardConfig{MaxQueries: intp(0)}, field: "dashboard.max_queries"},
		{name: "over hard cap queries", config: &DashboardConfig{MaxQueries: intp(HardMaxQueries + 1)}, field: "dashboard.max_queries"},
		{name: "bad timezone", config: &DashboardConfig{Timezone: str("local")}, field: "dashboard.timezone"},
		{name: "empty output dir", config: &DashboardConfig{OutputDir: str("")}, field: "dashboard.output_dir"},
		{name: "NUL output dir", config: &DashboardConfig{OutputDir: str("out\x00dir")}, field: "dashboard.output_dir"},
		{name: "long title suffix", config: &DashboardConfig{TitleSuffix: str(longSuffix)}, field: "dashboard.title_suffix"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := Resolve(test.config, nil)
			if err == nil {
				t.Fatalf("Resolve accepted invalid config")
			}
			var failures *ConfigErrors
			if !errors.As(err, &failures) {
				t.Fatalf("error is %T, want *ConfigErrors", err)
			}
			if !strings.Contains(err.Error(), CodeInvalidConfig) {
				t.Errorf("error lacks code %s: %v", CodeInvalidConfig, err)
			}
			if !strings.Contains(err.Error(), test.field) {
				t.Errorf("error lacks field path %q: %v", test.field, err)
			}
		})
	}
}

// TestErrorsRedactValues verifies no rejected value leaks into the error
// message, even when it looks like a secret or injection payload.
func TestErrorsRedactValues(t *testing.T) {
	canaries := []string{"s3cr3t-token", "DROP TABLE users", "'\"\n;", "password=letmein"}
	for _, canary := range canaries {
		config := &DashboardConfig{
			Refresh:                str(canary),
			DatasourceVariableName: str(canary),
			RateInterval:           str(canary),
			Timezone:               str(canary),
		}
		_, err := Resolve(config, nil)
		if err == nil {
			t.Fatalf("Resolve accepted canary %q", canary)
		}
		if strings.Contains(err.Error(), canary) {
			t.Errorf("error leaks rejected value %q: %v", canary, err)
		}
	}
}

// TestPolicyImmutability verifies the resolved policy never shares mutable
// state with the input configuration or overrides.
func TestPolicyImmutability(t *testing.T) {
	config := &DashboardConfig{OutputDir: str("first")}
	overrides := &Overrides{OutputDir: str("cli-first")}
	policy, err := Resolve(config, overrides)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	*config.OutputDir = "mutated-yaml"
	*overrides.OutputDir = "mutated-cli"
	if policy.OutputDir != "cli-first" {
		t.Errorf("policy shares input state; OutputDir = %q", policy.OutputDir)
	}
}

// TestPartialLimits verifies limit errors carry the exact offending path
// among multiple violations.
func TestPartialLimits(t *testing.T) {
	config := &DashboardConfig{
		MaxPanels:  intp(200),
		MaxQueries: intp(HardMaxQueries + 1),
	}
	_, err := Resolve(config, nil)
	if err == nil || !strings.Contains(err.Error(), "dashboard.max_queries") {
		t.Fatalf("expected max_queries violation, got %v", err)
	}
	if strings.Contains(err.Error(), "dashboard.max_panels") {
		t.Errorf("max_panels must not be reported: %v", err)
	}
}

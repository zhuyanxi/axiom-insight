package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/generator/schemacheck"
)

func TestDashboardDefaultAC1(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"dashboard", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "written dashboard.json") {
		t.Fatalf("stdout = %q, want dashboard summary", stdout.String())
	}
	outputDir := filepath.Join(root, "dashboards")
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("read dashboard output: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != dashboardFileName {
		t.Fatalf("output entries = %v, want only %s", entries, dashboardFileName)
	}
	contents, err := os.ReadFile(filepath.Join(outputDir, dashboardFileName))
	if err != nil {
		t.Fatalf("read dashboard: %v", err)
	}
	dashboard, err := model.Decode(contents)
	if err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if violations := model.Validate(dashboard); len(violations) > 0 {
		t.Fatalf("dashboard validation = %v", violations)
	}
}

func TestDashboardDryRunAC2(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"dashboard", root, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "planned dashboard.json") || !strings.Contains(stdout.String(), "sha256:") {
		t.Fatalf("stdout = %q, want planned hash summary", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "dashboards")); !os.IsNotExist(err) {
		t.Fatalf("dry-run output directory exists or stat failed: %v", err)
	}
}

func TestDashboardJSONReportAndVersion(t *testing.T) {
	t.Run("argument failure report", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", "one", "two", "--format", "json"}, &stdout, &stderr)
		if code != exitUsageError {
			t.Fatalf("exit code = %d, want %d", code, exitUsageError)
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil || report.Error.Code != cliUsageMessageCode || report.Error.Stage != "flags" {
			t.Fatalf("report = %+v", report)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--dry-run", "--format", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		var report dashboardReport
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("parse report: %v\n%s", err, stdout.String())
		}
		if report.SchemaVersion != dashboardReportSchema || report.Status != "success" || !report.DryRun {
			t.Fatalf("report = %+v", report)
		}
		if report.Dashboard == nil || report.Dashboard.SHA256 == "" || report.Dashboard.PanelCount == 0 {
			t.Fatalf("dashboard summary = %+v", report.Dashboard)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("version", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", "--version"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		for _, fragment := range []string{
			"si version:", "ir_schema_version:", "generator_schema_version:",
			"dashboard_schema_version:", "grafana_schema_version:",
		} {
			if !strings.Contains(stdout.String(), fragment) {
				t.Errorf("version output lacks %q: %s", fragment, stdout.String())
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("written report", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--format", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "success" || report.DryRun || len(report.Written) != 1 || report.Written[0] != dashboardFileName {
			t.Fatalf("report = %+v", report)
		}
		if report.Diagnostics == nil {
			t.Fatal("diagnostics must be explicit empty array")
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("overwrite failure report", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"dashboard", root}, &stdout, &stderr); code != 0 {
			t.Fatalf("initial exit code = %d; stderr = %s", code, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		code := run([]string{"dashboard", root, "--format", "json"}, &stdout, &stderr)
		if code != exitScanError {
			t.Fatalf("exit code = %d, want %d", code, exitScanError)
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil || report.Error.Code != dashboard.CodeOutputExists || report.Error.Stage != "commit" {
			t.Fatalf("report = %+v", report)
		}
		if len(report.Written) != 0 || stderr.Len() != 0 {
			t.Fatalf("written=%v stderr=%q", report.Written, stderr.String())
		}
	})
}

func TestDashboardJSONReportFailureMatrix(t *testing.T) {
	t.Run("non-strict warning", func(t *testing.T) {
		root := writeCLIProject(t, map[string]string{
			"go.mod": "module example.com/report-warning\n\ngo 1.26.1\n",
			"main.go": `package main

import "net/http"

func Call(target string) {
	_, _ = http.Get(target)
}
`,
			"si.yaml": "service:\n  name: bad service\n",
		})
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--dry-run", "--format", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "warning" || report.Error != nil || len(report.Diagnostics) == 0 || len(report.Written) != 0 {
			t.Fatalf("report = %+v", report)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("invalid dashboard config", func(t *testing.T) {
		root := writeCLIProject(t, map[string]string{
			"go.mod":  "module example.com/report-config\n\ngo 1.26.1\n",
			"main.go": "package main\n",
			"si.yaml": "dashboard:\n  refresh: 45s\n",
		})
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--format", "json"}, &stdout, &stderr)
		if code != exitUsageError {
			t.Fatalf("exit code = %d, want %d", code, exitUsageError)
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil || report.Error.Code != dashboard.CodeInvalidConfig || report.Error.Stage != "flags" {
			t.Fatalf("report = %+v", report)
		}
		if len(report.Written) != 0 || stderr.Len() != 0 {
			t.Fatalf("written=%v stderr=%q", report.Written, stderr.String())
		}
	})

	t.Run("strict warning", func(t *testing.T) {
		root := writeCLIProject(t, map[string]string{
			"go.mod": "module example.com/report-strict\n\ngo 1.26.1\n",
			"main.go": `package main

import "net/http"

func Call(target string) {
	_, _ = http.Get(target)
}
`,
			"si.yaml": "service:\n  name: bad service\n",
		})
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--strict", "--format", "json"}, &stdout, &stderr)
		if code != exitScanError {
			t.Fatalf("exit code = %d, want %d", code, exitScanError)
		}
		report := parseDashboardReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil || report.Error.Stage != "validate" || !strings.HasPrefix(report.Error.Code, "DASHBOARD_") {
			t.Fatalf("report = %+v", report)
		}
		if report.DryRun || len(report.Written) != 0 || stderr.Len() != 0 {
			t.Fatalf("dry_run=%v written=%v stderr=%q", report.DryRun, report.Written, stderr.String())
		}
	})
}

func TestDashboardReportContract(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard", root, "--dry-run", "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d; stderr = %s", code, stderr.String())
	}
	schemaPath := filepath.Join("..", "..", "schemas", "dashboard", "v1", "cli-dashboard-report.schema.json")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read report schema: %v", err)
	}
	if err := schemacheck.Validate(schema, stdout.Bytes()); err != nil {
		t.Fatalf("report schema validation failed: %v\n%s", err, stdout.String())
	}
}

func TestDashboardReportGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures", "http")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard", root, "--dry-run", "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d; stderr = %s", code, stderr.String())
	}
	goldenPath := filepath.Join("..", "..", "testdata", "dashboard", "cli", "expected-report.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read report golden: %v", err)
	}
	if !bytes.Equal(stdout.Bytes(), append(expected, '\n')) && !bytes.Equal(stdout.Bytes(), expected) {
		t.Fatalf("dashboard report differs from %s", goldenPath)
	}
}

func TestDashboardReportOrderingRedactionAndLimit(t *testing.T) {
	report := &dashboardReport{
		SchemaVersion:          dashboardReportSchema,
		Status:                 "warning",
		CLIVersion:             cliVersion,
		IRSchemaVersion:        "v1",
		GeneratorSchemaVersion: "v0.2.0",
		DashboardSchemaVersion: model.ContractVersion,
		GrafanaSchemaVersion:   model.SchemaVersion,
		CompletedStage:         "validate",
		Written:                []string{},
		Diagnostics: []dashboardReportDiagnostic{
			{Code: dashboard.CodeEmptyCategory, Severity: "info", Category: "http", TargetID: "z"},
			{Code: dashboard.CodeMissingRequiredMetric, Severity: "warning", Category: "catalog", TargetID: "b"},
			{Code: dashboard.CodeMissingRequiredMetric, Severity: "warning", Category: "catalog", TargetID: "a"},
		},
	}
	contents, err := marshalDashboardReport(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded dashboardReport
	if err := json.Unmarshal(contents, &decoded); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if got := decoded.Diagnostics[0].TargetID; got != "a" {
		t.Fatalf("first diagnostic target = %q, want a", got)
	}
	if got := decoded.Diagnostics[2].Severity; got != "info" {
		t.Fatalf("last diagnostic severity = %q, want info", got)
	}
	canary := "canary-password-hunter2"
	redacted := makeDashboardReportError(errors.New(canary), "render")
	if strings.Contains(redacted.Message, canary) {
		t.Fatalf("report error leaks canary: %q", redacted.Message)
	}

	large := *report
	large.Diagnostics = make([]dashboardReportDiagnostic, 300)
	for index := range large.Diagnostics {
		large.Diagnostics[index] = dashboardReportDiagnostic{
			Code: dashboard.CodeSensitiveValueDropped, Severity: "warning",
			Message: strings.Repeat("x", maxDashboardReportText),
		}
	}
	if _, err := marshalDashboardReport(&large); err == nil {
		t.Fatal("oversized report accepted")
	}
	tooMany := *report
	tooMany.Diagnostics = make([]dashboardReportDiagnostic, maxDashboardReportDiagnostics+1)
	contents, err = marshalDashboardReport(&tooMany)
	if err != nil {
		t.Fatalf("bounded report: %v", err)
	}
	if len(tooMany.Diagnostics) != maxDashboardReportDiagnostics {
		t.Fatalf("diagnostics = %d, want %d", len(tooMany.Diagnostics), maxDashboardReportDiagnostics)
	}

	oversized := &dashboardReport{
		SchemaVersion:  dashboardReportSchema,
		Status:         "failure",
		CompletedStage: "flags",
		Written:        []string{},
		Diagnostics:    make([]dashboardReportDiagnostic, 300),
	}
	for index := range oversized.Diagnostics {
		oversized.Diagnostics[index] = dashboardReportDiagnostic{
			Code: dashboard.CodeSensitiveValueDropped, Severity: "warning",
			Message: strings.Repeat("x", maxDashboardReportText),
		}
	}
	if _, err := marshalDashboardReport(oversized); err == nil {
		t.Fatal("oversized report accepted before fallback")
	}
	resetDashboardReportForEncodingFailure(oversized)
	contents, err = marshalDashboardReport(oversized)
	if err != nil || len(contents) > maxDashboardReportBytes {
		t.Fatalf("fallback report err=%v size=%d", err, len(contents))
	}
}

func TestDashboardReportSchemaStateRules(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "schemas", "dashboard", "v1", "cli-dashboard-report.schema.json")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read report schema: %v", err)
	}
	valid := []byte(`{"schema_version":"cli.dashboard_report/v1","status":"failure","cli_version":"v0.2.0","ir_schema_version":"v1","generator_schema_version":"v0.2.0","dashboard_schema_version":"grafana.dashboard/v1","grafana_schema_version":41,"completed_stage":"flags","dry_run":false,"written":[],"diagnostics":[],"error":{"code":"CLI_INVALID_ARGUMENT","stage":"flags","message":"flags stage failed (CLI_INVALID_ARGUMENT)"}}`)
	if err := schemacheck.Validate(schema, valid); err != nil {
		t.Fatalf("valid failure report rejected: %v", err)
	}
	invalid := []byte(`{"schema_version":"cli.dashboard_report/v1","status":"failure","cli_version":"v0.2.0","ir_schema_version":"v1","generator_schema_version":"v0.2.0","dashboard_schema_version":"grafana.dashboard/v1","grafana_schema_version":41,"completed_stage":"flags","dry_run":true,"written":["dashboard.json"],"diagnostics":[]}`)
	if err := schemacheck.Validate(schema, invalid); err == nil {
		t.Fatal("invalid report state accepted")
	}
}

func parseDashboardReport(t *testing.T, contents []byte) dashboardReport {
	t.Helper()
	var report dashboardReport
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("parse dashboard report: %v\n%s", err, contents)
	}
	return report
}

func TestDashboardOutputProtectionAC3(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("initial dashboard exit code = %d; stderr = %s", code, stderr.String())
	}
	targetPath := filepath.Join(root, "dashboards", dashboardFileName)
	original, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read initial dashboard: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"dashboard", root}, &stdout, &stderr); code != exitScanError {
		t.Fatalf("overwrite exit code = %d, want %d", code, exitScanError)
	}
	if !strings.Contains(stderr.String(), dashboard.CodeOutputExists) {
		t.Fatalf("stderr = %q, want %s", stderr.String(), dashboard.CodeOutputExists)
	}
	unchanged, _ := os.ReadFile(targetPath)
	if !bytes.Equal(original, unchanged) {
		t.Fatal("existing dashboard changed without --force")
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"dashboard", root, "--force"}, &stdout, &stderr); code != 0 {
		t.Fatalf("force exit code = %d; stderr = %s", code, stderr.String())
	}
	forced, _ := os.ReadFile(targetPath)
	if !bytes.Equal(original, forced) {
		t.Fatal("deterministic dashboard bytes changed after --force")
	}
}

func TestDashboardUnsafeTargetsAC3(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := generateFixture(t)
		outputDir := filepath.Join(root, "dashboards")
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		outside := filepath.Join(t.TempDir(), dashboardFileName)
		if err := os.WriteFile(outside, []byte("external"), 0o600); err != nil {
			t.Fatalf("seed outside: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(outputDir, dashboardFileName)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--force"}, &stdout, &stderr)
		if code != exitScanError || !strings.Contains(stderr.String(), dashboard.CodeUnsafeTarget) {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		contents, _ := os.ReadFile(outside)
		if string(contents) != "external" {
			t.Fatal("symlink target changed")
		}
	})

	t.Run("hard link", func(t *testing.T) {
		root := generateFixture(t)
		outputDir := filepath.Join(root, "dashboards")
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		outside := filepath.Join(t.TempDir(), "external.json")
		if err := os.WriteFile(outside, []byte("external"), 0o600); err != nil {
			t.Fatalf("seed outside: %v", err)
		}
		if err := os.Link(outside, filepath.Join(outputDir, dashboardFileName)); err != nil {
			t.Skipf("hard links unavailable: %v", err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"dashboard", root, "--force"}, &stdout, &stderr)
		if code != exitScanError || !strings.Contains(stderr.String(), dashboard.CodeUnsafeTarget) {
			t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
		}
		contents, _ := os.ReadFile(outside)
		if string(contents) != "external" {
			t.Fatal("hard-link target changed")
		}
	})
}

func TestDashboardExistingLockAC3(t *testing.T) {
	root := generateFixture(t)
	outputDir := filepath.Join(root, "dashboards")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetPath := filepath.Join(outputDir, dashboardFileName)
	if err := os.WriteFile(targetPath, []byte("existing"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, dashboardLockName), []byte("locked"), 0o600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"dashboard", root, "--force"}, &stdout, &stderr)
	if code != exitScanError || !strings.Contains(stderr.String(), dashboard.CodeOutputExists) {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	contents, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(contents) != "existing" {
		t.Fatal("existing dashboard changed while lock was held")
	}
}

func TestDashboardStrictWarningAC4(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/dashboard-strict\n\ngo 1.26.1\n",
		"main.go": `package main

import "net/http"

func Call(target string) {
	_, _ = http.Get(target)
}
`,
		"si.yaml": "service:\n  name: bad service\n",
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"dashboard", root, "--strict"}, &stdout, &stderr)
	if code != exitScanError {
		t.Fatalf("exit code = %d, want %d; stderr = %s", code, exitScanError, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "dashboards")); err == nil {
		t.Fatal("strict failure must not create dashboard output")
	}
	if !strings.Contains(stderr.String(), "DASHBOARD_") {
		t.Fatalf("stderr = %q, want dashboard diagnostic code", stderr.String())
	}
}

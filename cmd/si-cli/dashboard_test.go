package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
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

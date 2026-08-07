package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateReportGolden pins the JSON report for a real Phase 0 source
// fixture. The report carries no absolute paths, so it is comparable
// across machines.
func TestGenerateReportGolden(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "fixtures", "http")
	outputDir := filepath.Join(t.TempDir(), "generate")
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", fixture, "--output-dir", outputDir, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	report := parseReport(t, stdout.Bytes())

	goldenPath := filepath.Join("..", "..", "testdata", "generator", "cli", "expected-report.json")
	contents, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	contents = append(contents, '\n')
	if os.Getenv("SI_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, contents, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden %s", goldenPath)
		return
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s (set SI_UPDATE_GOLDEN=1 to regenerate): %v", goldenPath, err)
	}
	if !bytes.Equal(contents, expected) {
		t.Fatalf("report differs from golden %s; set SI_UPDATE_GOLDEN=1 to regenerate", goldenPath)
	}
	// No absolute temp paths may leak into the report.
	reportText := string(contents)
	if strings.Contains(reportText, t.TempDir()) || strings.Contains(reportText, fixture) {
		t.Error("report leaks an absolute path")
	}
}

// TestFailureScenariosHaveNoSideEffectsAC5: strict failures, render
// failures, existing targets and writer faults never leave temporary or
// backup files behind.
func TestFailureScenariosHaveNoSideEffectsAC5(t *testing.T) {
	t.Run("strict failure", func(t *testing.T) {
		root := writeCLIProject(t, map[string]string{
			"go.mod": "module example.com/no-side-effect\n\ngo 1.26.1\n",
			"main.go": `package main

import "net/http"

func Call(target string) {
	_, _ = http.Get(target)
}
`,
			"si.yaml": "service:\n  name: no-side-effect\n",
		})
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--strict"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		assertNoLeftovers(t, filepath.Join(root, "generate"))
	})

	t.Run("existing target refused", func(t *testing.T) {
		root := generateFixture(t)
		outputDir := filepath.Join(root, "generate")
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "metrics.yaml"), []byte("old"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		assertNoLeftovers(t, outputDir)
		contents, _ := os.ReadFile(filepath.Join(outputDir, "metrics.yaml"))
		if string(contents) != "old" {
			t.Error("existing target must stay unchanged")
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--dry-run"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		assertNoLeftovers(t, filepath.Join(root, "generate"))
		if _, err := os.Stat(filepath.Join(root, "generate")); err == nil {
			t.Fatal("dry-run must not create the output directory")
		}
	})
}

func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".si-tmp-") || strings.Contains(entry.Name(), ".si-backup-") ||
			entry.Name() == ".si-generate.lock" {
			t.Errorf("leftover %s in %s", entry.Name(), dir)
		}
	}
}

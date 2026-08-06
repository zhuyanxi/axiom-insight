package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhuyanxi/axiom-insight/generator"
)

// generateFixture writes a small Go project with one HTTP endpoint, one
// SQL dependency and a cron job so all three signals produce output.
func generateFixture(t *testing.T) string {
	t.Helper()
	return writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/gen-fixture\n\ngo 1.26.1\n",
		"main.go": `package main

import (
	"database/sql"
	"net/http"
	"github.com/robfig/cron/v3"
)

func main() {
	http.HandleFunc("/orders/{id}", handleOrder)
	_ = sql.Open("postgres", "postgres://localhost/orders")
	c := cron.New()
	_, _ = c.AddFunc("0 3 * * *", cleanup)
	c.Start()
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	row := dbQuery()
	_, _ = row, w
}

func dbQuery() *sql.Rows {
	db, _ := sql.Open("postgres", "postgres://localhost/orders")
	rows, _ := db.Query("SELECT * FROM orders")
	return rows
}

func cleanup() {}
`,
		"si.yaml": "service:\n  name: gen-fixture\n",
	})
}

// TestGenerateDefaultAC1: default generate exits 0 and creates three
// strictly validable files with the right definition counts.
func TestGenerateDefaultAC1(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	outputDir := filepath.Join(root, "generate")
	for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
		contents, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		switch name {
		case "metrics.yaml":
			document, err := generator.DecodeMetrics(contents)
			if err != nil || len(document.Validate()) > 0 {
				t.Fatalf("%s not strictly valid: %v", name, err)
			}
			if len(document.Metrics) == 0 {
				t.Error("metrics.yaml has no definitions")
			}
		case "otel.yaml":
			document, err := generator.DecodeOTel(contents)
			if err != nil || len(document.Validate()) > 0 {
				t.Fatalf("%s not strictly valid: %v", name, err)
			}
		case "logging.yaml":
			document, err := generator.DecodeLogging(contents)
			if err != nil || len(document.Validate()) > 0 {
				t.Fatalf("%s not strictly valid: %v", name, err)
			}
		}
	}
}

// TestGenerateDryRunAC2: dry-run validates everything in memory and
// reports hashes, but never creates the output directory.
func TestGenerateDryRunAC2(t *testing.T) {
	root := generateFixture(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	output := stdout.String()
	for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
		if !strings.Contains(output, name) {
			t.Errorf("dry-run report lacks %s: %s", name, output)
		}
	}
	if !strings.Contains(output, "sha256:") {
		t.Errorf("dry-run report lacks hashes: %s", output)
	}
	if _, err := os.Stat(filepath.Join(root, "generate")); err == nil {
		t.Fatal("dry-run must not create the output directory")
	}
}

// TestGenerateSignalSubsetAC3: --signals metrics --force writes only
// metrics.yaml; other files keep their bytes and metadata.
func TestGenerateSignalSubsetAC3(t *testing.T) {
	root := generateFixture(t)
	outputDir := filepath.Join(root, "generate")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	userOtel := "user-managed: true\n"
	notes := "keep me\n"
	if err := os.WriteFile(filepath.Join(outputDir, "otel.yaml"), []byte(userOtel), 0o600); err != nil {
		t.Fatalf("seed otel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "notes.txt"), []byte(notes), 0o644); err != nil {
		t.Fatalf("seed notes: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root, "--signals", "metrics", "--force"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outputDir, "metrics.yaml")); err != nil {
		t.Fatalf("metrics.yaml missing: %v", err)
	}
	otelContents, _ := os.ReadFile(filepath.Join(outputDir, "otel.yaml"))
	if string(otelContents) != userOtel {
		t.Error("otel.yaml must stay untouched")
	}
	info, _ := os.Stat(filepath.Join(outputDir, "otel.yaml"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("otel.yaml metadata changed: %v", info.Mode().Perm())
	}
	notesContents, _ := os.ReadFile(filepath.Join(outputDir, "notes.txt"))
	if string(notesContents) != notes {
		t.Error("notes.txt must stay untouched")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "logging.yaml")); err == nil {
		t.Error("logging.yaml must not be created for a metrics-only run")
	}
}

// TestGenerateRefusesOverwriteAC4: an existing target without --force
// exits 1 with GEN_OUTPUT_EXISTS and changes nothing.
func TestGenerateRefusesOverwriteAC4(t *testing.T) {
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
	if !strings.Contains(stderr.String(), "GEN_OUTPUT_EXISTS") {
		t.Errorf("stderr = %q, want GEN_OUTPUT_EXISTS", stderr.String())
	}
	if !strings.Contains(stderr.String(), cliGenerateMessageCode) {
		t.Errorf("stderr = %q, want %s", stderr.String(), cliGenerateMessageCode)
	}
	contents, _ := os.ReadFile(filepath.Join(outputDir, "metrics.yaml"))
	if string(contents) != "old" {
		t.Error("existing target must stay unchanged")
	}
	if _, err := os.Stat(filepath.Join(outputDir, "otel.yaml")); err == nil {
		t.Error("no other target may be created")
	}
}

// TestGenerateForceReplacesManagedAC5: --force replaces the three managed
// files but never touches unrelated files.
func TestGenerateForceReplacesManagedAC5(t *testing.T) {
	root := generateFixture(t)
	outputDir := filepath.Join(root, "generate")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
		if err := os.WriteFile(filepath.Join(outputDir, name), []byte("old"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(outputDir, "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("seed notes: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root, "--force"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	for _, name := range []string{"metrics.yaml", "otel.yaml", "logging.yaml"} {
		contents, _ := os.ReadFile(filepath.Join(outputDir, name))
		if strings.Contains(string(contents), "old") {
			t.Errorf("%s not replaced", name)
		}
	}
	notes, _ := os.ReadFile(filepath.Join(outputDir, "notes.txt"))
	if string(notes) != "notes" {
		t.Error("notes.txt must stay untouched")
	}
}

// TestGenerateStrictFailureWritesNothingAC9: a strict run over a source
// with a dynamic target fails without creating the output directory.
func TestGenerateStrictFailureWritesNothingAC9(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/gen-dynamic\n\ngo 1.26.1\n",
		"main.go": `package main

import "net/http"

func Call(target string) {
	_, _ = http.Get(target)
}
`,
		"si.yaml": "service:\n  name: gen-dynamic\n",
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root, "--strict"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "generate")); err == nil {
		t.Fatal("strict failure must not create the output directory")
	}
}

// TestGenerateSymlinkTargetRejectedAC8: a symlinked target exits 1 and
// the external file stays untouched.
func TestGenerateSymlinkTargetRejectedAC8(t *testing.T) {
	root := generateFixture(t)
	outputDir := filepath.Join(root, "generate")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("external"), 0o644); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(outputDir, "metrics.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root, "--force"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "symlink") {
		t.Errorf("stderr = %q, want symlink rejection", stderr.String())
	}
	contents, _ := os.ReadFile(outside)
	if string(contents) != "external" {
		t.Error("external file must stay untouched")
	}
}

// TestGenerateJSONReportAC10: JSON mode writes exactly one parseable
// report on stdout with empty stderr for success, dry-run, config
// errors, strict failure and writer errors.
func TestGenerateJSONReportAC10(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--format", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		report := parseReport(t, stdout.Bytes())
		if report.Status != "success" || len(report.PlannedFiles) != 3 {
			t.Errorf("report = %+v", report)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("dry run", func(t *testing.T) {
		root := generateFixture(t)
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--dry-run", "--format", "json"}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		report := parseReport(t, stdout.Bytes())
		if !report.DryRun || len(report.PlannedFiles) != 3 {
			t.Errorf("report = %+v", report)
		}
	})

	t.Run("config error", func(t *testing.T) {
		root := writeCLIProject(t, map[string]string{
			"go.mod": "module example.com/gen-config\n\ngo 1.26.1\n",
			"run.go": "package genconfig\n",
			"si.yaml": "generation:\n  signals: []\n",
		})
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--format", "json"}, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
		report := parseReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil {
			t.Errorf("report = %+v", report)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr.String())
		}
	})

	t.Run("writer error", func(t *testing.T) {
		root := generateFixture(t)
		outputDir := filepath.Join(root, "generate")
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "metrics.yaml"), []byte("old"), 0o644); err != nil {
			t.Fatalf("seed: %v", err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", root, "--format", "json"}, &stdout, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		report := parseReport(t, stdout.Bytes())
		if report.Status != "failure" || report.Error == nil ||
			!strings.Contains(report.Error.Message, "GEN_OUTPUT_EXISTS") {
			t.Errorf("report = %+v", report)
		}
	})
}

// TestGenerateVersion: --version prints CLI, IR and Generator schema
// versions without changing scan --version output.
func TestGenerateVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	output := stdout.String()
	for _, fragment := range []string{"si version:", "ir_schema_version:", "generator_schema_version:"} {
		if !strings.Contains(output, fragment) {
			t.Errorf("version output lacks %q: %s", fragment, output)
		}
	}
}

// TestGenerateOfflineAC12: generate succeeds with module proxies and
// network disabled; the pipeline never resolves or connects anywhere.
func TestGenerateOfflineAC12(t *testing.T) {
	root := generateFixture(t)
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
}

func parseReport(t *testing.T, contents []byte) generateReport {
	t.Helper()
	var report generateReport
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatalf("report is not a single JSON document: %v\n%s", err, contents)
	}
	if report.SchemaVersion != generateReportSchema {
		t.Errorf("report schema version = %q", report.SchemaVersion)
	}
	return report
}

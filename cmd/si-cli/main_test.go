package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandErrorIncludesStableMessageCode(t *testing.T) {
	err := internalFailure(errors.New("unexpected failure"))
	if got, want := err.Error(), cliInternalMessageCode+": unexpected failure"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRunScanPrintsStableTextSummary(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod":  "module example.com/cli-text\n\ngo 1.26.1\n",
		"run.go":  "package clitext\n\nfunc Run() {}\n",
		"si.yaml": "service:\n  name: cli-text-service\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "service: cli-text-service") {
		t.Fatalf("text output missing service: %s", output)
	}
	for _, name := range []string{
		"http_handlers",
		"grpc_handlers",
		"cron_jobs",
		"kafka_consumers",
		"kafka_producers",
		"sql",
		"redis",
		"http_clients",
		"rpc_clients",
		"diagnostics",
	} {
		if !strings.Contains(output, name+": 0") {
			t.Fatalf("text output missing zero summary %q: %s", name, output)
		}
	}
}

func TestRunScanVersionPrintsCLIVersionAndIRSchemaVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", "--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	want := "si version: " + defaultCLIVersion + "\nir_schema_version: v1\n"
	if stdout.String() != want {
		t.Fatalf("version output = %q, want %q", stdout.String(), want)
	}
}

func TestRunScanJSONContainsIRSummaryAndDiagnostics(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/cli-json\n\ngo 1.26.1\n",
		"run.go": "package clijson\n\nfunc Run() {}\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root, "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	var result struct {
		SchemaVersion string `json:"schema_version"`
		Summary       []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"summary"`
		Document struct {
			Service struct {
				Name string `json:"name"`
			} `json:"service"`
		} `json:"document"`
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON output: %v\n%s", err, stdout.String())
	}
	if result.SchemaVersion != "v1" {
		t.Fatalf("schema version = %q, want v1", result.SchemaVersion)
	}
	if result.Document.Service.Name != "cli-json" {
		t.Fatalf("document service name = %q, want cli-json", result.Document.Service.Name)
	}
	if len(result.Summary) != 10 {
		t.Fatalf("summary item count = %d, want 10", len(result.Summary))
	}
	for _, item := range result.Summary {
		if item.Count != 0 {
			t.Fatalf("summary item %q = %d, want 0", item.Name, item.Count)
		}
	}
	if result.Diagnostics == nil {
		t.Fatal("diagnostics field missing or null")
	}
}

func TestRunScanOutputFileAndIncludeTestsConfig(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod":      "module example.com/cli-output\n\ngo 1.26.1\n",
		"run.go":      "package clioutput\n\nfunc Run() {}\n",
		"run_test.go": "package clioutput\n\nfunc TestRun() {}\n",
		"si.yaml":     "include_tests: true\n",
	})
	outputPath := filepath.Join(t.TempDir(), "scan.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root, "--format", "json", "--output", outputPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty when --output is set", stdout.String())
	}
	contents, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	var result struct {
		Document struct {
			Functions []struct {
				Name string `json:"name"`
			} `json:"functions"`
		} `json:"document"`
	}
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("parse output file: %v", err)
	}
	if !hasJSONFunction(result.Document.Functions, "TestRun") {
		t.Fatalf("output file omitted test function: %s", contents)
	}
}

func TestRunScanRejectsInvalidPathWithUsageExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)
	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if !strings.Contains(stderr.String(), "invalid source path") {
		t.Fatalf("stderr = %q, want invalid source path", stderr.String())
	}
	if !strings.Contains(stderr.String(), cliUsageMessageCode) {
		t.Fatalf("stderr = %q, want message code %s", stderr.String(), cliUsageMessageCode)
	}
}

func TestRunScanRejectsInvalidConfigWithUsageExitCode(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod":  "module example.com/cli-config\n\ngo 1.26.1\n",
		"run.go":  "package cliconfig\n\nfunc Run() {}\n",
		"si.yaml": "language: rust\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root}, &stdout, &stderr)
	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if !strings.Contains(stderr.String(), "only go is available") {
		t.Fatalf("stderr = %q, want language validation error", stderr.String())
	}
	if !strings.Contains(stderr.String(), cliUsageMessageCode) {
		t.Fatalf("stderr = %q, want message code %s", stderr.String(), cliUsageMessageCode)
	}
}

func TestRunScanAcceptsValidGenerationNode(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/cli-generation\n\ngo 1.26.1\n",
		"run.go": "package cligeneration\n\nfunc Run() {}\n",
		"si.yaml": "service:\n  name: gen-service\ngeneration:\n  signals: [metrics, tracing, logging]\n  metrics:\n    histogram_buckets_seconds: [0.1, 0.5, 1]\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "service: gen-service") {
		t.Fatalf("text output missing service: %s", stdout.String())
	}
}

func TestRunScanRejectsUnknownGenerationFieldWithConfigExitCode(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/cli-generation-unknown\n\ngo 1.26.1\n",
		"run.go": "package cligenerationunknown\n\nfunc Run() {}\n",
		"si.yaml": "generation:\n  semantic_conventions_version: 1.38.0\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root}, &stdout, &stderr)
	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if !strings.Contains(stderr.String(), "GEN_INVALID_CONFIG") {
		t.Fatalf("stderr = %q, want GEN_INVALID_CONFIG", stderr.String())
	}
	if !strings.Contains(stderr.String(), "semantic_conventions_version") {
		t.Fatalf("stderr = %q, want unknown field name", stderr.String())
	}
	if !strings.Contains(stderr.String(), cliUsageMessageCode) {
		t.Fatalf("stderr = %q, want message code %s", stderr.String(), cliUsageMessageCode)
	}
}

func TestRunScanRejectsInvalidGenerationRangeWithFieldPath(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/cli-generation-range\n\ngo 1.26.1\n",
		"run.go": "package cligenerationrange\n\nfunc Run() {}\n",
		"si.yaml": "generation:\n  metrics:\n    histogram_buckets_seconds: [0.5, 0.1]\n",
	})

	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root}, &stdout, &stderr)
	if code != exitUsageError {
		t.Fatalf("exit code = %d, want %d", code, exitUsageError)
	}
	if !strings.Contains(stderr.String(), "generation.metrics.histogram_buckets_seconds[1]") {
		t.Fatalf("stderr = %q, want exact field path", stderr.String())
	}
}

func TestRunScanOutputWriteFailureUsesScanExitCode(t *testing.T) {
	root := writeCLIProject(t, map[string]string{
		"go.mod": "module example.com/cli-write-error\n\ngo 1.26.1\n",
		"run.go": "package cliwriteerror\n\nfunc Run() {}\n",
	})
	outputPath := filepath.Join(t.TempDir(), "missing", "scan.json")
	var stdout, stderr bytes.Buffer
	code := run([]string{"scan", root, "--format", "json", "--output", outputPath}, &stdout, &stderr)
	if code != exitScanError {
		t.Fatalf("exit code = %d, want %d", code, exitScanError)
	}
	if !strings.Contains(stderr.String(), "write scan result") {
		t.Fatalf("stderr = %q, want write failure", stderr.String())
	}
	if !strings.Contains(stderr.String(), cliScanMessageCode) {
		t.Fatalf("stderr = %q, want message code %s", stderr.String(), cliScanMessageCode)
	}
}

func hasJSONFunction(functions []struct {
	Name string `json:"name"`
}, wanted string) bool {
	for _, function := range functions {
		if function.Name == wanted {
			return true
		}
	}
	return false
}

func writeCLIProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture file: %v", err)
		}
	}
	return root
}

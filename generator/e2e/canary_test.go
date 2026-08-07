package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
)

// canaries are the sensitive fixture values that must never survive any
// serialized output or error channel.
var canaries = []string{
	"canary-password-hunter2",
	"canary-redis-key-7f3a",
	"canary-user",
	"canary-pass",
	"canary.example",
	"canary-fragment",
	"canary-topic-9f8e7d6c",
	"canary-payload-b1a2c3",
	"canary-email-4d5e6f@example.com",
}

// TestCanaryFullChainAC4: the sensitive fixture drives the plan, the
// three renders and the CLI; no canary appears in any serialized output,
// stdout, stderr or error string.
func TestCanaryFullChainAC4(t *testing.T) {
	document := loadIRFixture(t, "sensitive-values.json")

	// Stage 1: plan, serialized as protojson.
	plan, outputs := planAndRenderDocument(t, document, nil)
	planBytes, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	assertNoCanaries(t, "plan", planBytes)

	// Stage 2: the three rendered YAML files.
	for name, rendered := range outputs {
		assertNoCanaries(t, name, rendered)
	}

	// Stage 3: CLI text and JSON stdout/stderr, including failure paths.
	cli := buildCLI(t)
	for _, args := range [][]string{
		{"generate", ".", "--format", "text"},
		{"generate", ".", "--format", "json"},
		{"generate", ".", "--dry-run", "--format", "json"},
	} {
		command := exec.Command(cli, args...)
		command.Dir = t.TempDir()
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		_ = command.Run()
		assertNoCanaries(t, strings.Join(args, " ")+" stdout", stdout.Bytes())
		assertNoCanaries(t, strings.Join(args, " ")+" stderr", stderr.Bytes())
	}
}

// buildCLI builds the si binary once per test run.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "si")
	command := exec.Command("go", "build", "-o", bin, "github.com/zhuyanxi/axiom-insight/cmd/si-cli")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build si: %v\n%s", err, output)
	}
	return bin
}

func assertNoCanaries(t *testing.T, channel string, contents []byte) {
	t.Helper()
	text := string(contents)
	for _, canary := range canaries {
		if strings.Contains(text, canary) {
			t.Fatalf("canary %q leaked into %s", canary, channel)
		}
	}
}

var _ = os.Getenv

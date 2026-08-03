package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// updateGoldenEnv switches golden-file regeneration on for this test only.
// Normal test runs never rewrite committed golden files.
const updateGoldenEnv = "SI_UPDATE_GOLDEN"

func TestGoldenDocuments(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		golden  string
		render  func([]byte) ([]byte, error)
	}{
		{
			name:    "metrics",
			fixture: "valid/metrics.yaml",
			golden:  "golden/metrics.yaml",
			render: func(data []byte) ([]byte, error) {
				document, err := DecodeMetrics(data)
				if err != nil {
					return nil, err
				}
				return RenderMetrics(document)
			},
		},
		{
			name:    "otel",
			fixture: "valid/otel.yaml",
			golden:  "golden/otel.yaml",
			render: func(data []byte) ([]byte, error) {
				document, err := DecodeOTel(data)
				if err != nil {
					return nil, err
				}
				return RenderOTel(document)
			},
		},
		{
			name:    "logging",
			fixture: "valid/logging.yaml",
			golden:  "golden/logging.yaml",
			render: func(data []byte) ([]byte, error) {
				document, err := DecodeLogging(data)
				if err != nil {
					return nil, err
				}
				return RenderLogging(document)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := test.render(readFixture(t, test.fixture))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			goldenPath := fixturePath(test.golden)
			if os.Getenv(updateGoldenEnv) != "" {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("create golden dir: %v", err)
				}
				if err := os.WriteFile(goldenPath, rendered, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated golden file %s", goldenPath)
				return
			}
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden file %s (set %s=1 to regenerate): %v", goldenPath, updateGoldenEnv, err)
			}
			if !bytes.Equal(rendered, expected) {
				t.Fatalf("rendered output differs from golden %s; set %s=1 to regenerate", goldenPath, updateGoldenEnv)
			}
		})
	}
}

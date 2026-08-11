package dashboard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocumentationConfigExamples validates every ```yaml fenced block in
// docs/15-dashboard-config.md: blocks before the "无效示例" heading must
// strictly decode and resolve; blocks after it must fail with a config
// error that carries the dashboard path.
func TestDocumentationConfigExamples(t *testing.T) {
	content := readConfigurationDocument(t)
	invalidHeading := "## 无效示例"
	invalidIndex := strings.Index(content, invalidHeading)
	if invalidIndex < 0 {
		t.Fatalf("documentation lacks heading %q", invalidHeading)
	}
	validBlocks := yamlFences(content[:invalidIndex])
	invalidBlocks := yamlFences(content[invalidIndex:])

	if len(validBlocks) < 2 {
		t.Fatalf("expected at least 2 valid configuration blocks, found %d", len(validBlocks))
	}
	for index, block := range validBlocks {
		config, err := DecodeDashboardConfigFile([]byte(block))
		if err != nil {
			t.Errorf("valid block %d does not decode strictly: %v", index+1, err)
			continue
		}
		if config == nil {
			t.Errorf("valid block %d has no dashboard node", index+1)
			continue
		}
		if _, err := Resolve(config, nil); err != nil {
			t.Errorf("valid block %d fails resolution: %v", index+1, err)
		}
	}

	if len(invalidBlocks) < 8 {
		t.Fatalf("expected at least 8 invalid configuration blocks, found %d", len(invalidBlocks))
	}
	for index, block := range invalidBlocks {
		config, decodeErr := DecodeDashboardConfigFile([]byte(block))
		resolveErr := error(nil)
		if decodeErr == nil && config != nil {
			_, resolveErr = Resolve(config, nil)
		}
		if decodeErr == nil && resolveErr == nil {
			t.Errorf("invalid block %d was accepted", index+1)
			continue
		}
		message := ""
		if decodeErr != nil {
			message = decodeErr.Error()
		} else {
			message = resolveErr.Error()
		}
		if !strings.Contains(message, "dashboard") {
			t.Errorf("invalid block %d error lacks dashboard context: %q", index+1, message)
		}
	}
}

func readConfigurationDocument(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "docs", "15-dashboard-config.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration documentation: %v", err)
	}
	return string(data)
}

var configFencePattern = regexp.MustCompile("(?s)```yaml\n(.*?)```")

func yamlFences(content string) []string {
	matches := configFencePattern.FindAllStringSubmatch(content, -1)
	blocks := make([]string, 0, len(matches))
	for _, match := range matches {
		blocks = append(blocks, match[1])
	}
	return blocks
}

package pipeline

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	observabilityv1 "github.com/zhuyanxi/axiom-insight/ir/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

const p213GoldenUpdateEnv = "SI_UPDATE_GOLDEN"

// TestP213DashboardGoldens locks the four supported Phase 2 output modes.
// Normal test runs only read committed files; SI_UPDATE_GOLDEN=1 is the
// explicit snapshot update path.
func TestP213DashboardGoldens(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		policy func(t *testing.T) dashboard.DashboardPolicy
		mutate func(*dashboard.DashboardCatalog)
	}{
		{name: "composite", policy: resolvePolicy},
		{name: "no-trace-links", policy: p213NoTraceLinksPolicy},
		{name: "no-client-dependencies", policy: p213NoClientDependenciesPolicy},
		{name: "degraded", policy: resolvePolicy, mutate: p213DegradeCatalog},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			catalog := fullCatalog()
			if scenario.mutate != nil {
				scenario.mutate(catalog)
			}
			plan, err := Build(catalog, scenario.policy(t))
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			result, err := Render(plan)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			goldenPath := filepath.Join("..", "..", "testdata", "dashboard", "v1", "golden", scenario.name, "dashboard.json")
			if os.Getenv(p213GoldenUpdateEnv) != "" {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatalf("create golden directory: %v", err)
				}
				if err := os.WriteFile(goldenPath, result.Bytes, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated dashboard golden %s", goldenPath)
				return
			}
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s (set %s=1 to regenerate): %v", goldenPath, p213GoldenUpdateEnv, err)
			}
			if bytes.Equal(expected, result.Bytes) {
				return
			}
			differences, diffErr := Diff(expected, result.Bytes)
			if diffErr != nil {
				t.Fatalf("golden differs and semantic diff failed: %v", diffErr)
			}
			t.Fatalf("dashboard golden differs at %v; set %s=1 to regenerate", differences, p213GoldenUpdateEnv)
		})
	}
}

// TestP213DeterminismAndPermutations verifies output bytes, hash and
// diagnostics stay stable across repeated runs and fixed input orderings.
func TestP213DeterminismAndPermutations(t *testing.T) {
	policy := resolvePolicy(t)
	baseline := p213Render(t, fullCatalog(), policy)
	for run := 0; run < 10; run++ {
		got := p213Render(t, fullCatalog(), policy)
		if !bytes.Equal(got.Bytes, baseline.Bytes) || got.SHA256 != baseline.SHA256 {
			t.Fatalf("run %d changed dashboard bytes or hash", run)
		}
	}

	for round := 1; round <= 25; round++ {
		catalog := fullCatalog()
		p213PermuteItems(catalog.Items, round)
		got := p213Render(t, catalog, policy)
		if !bytes.Equal(got.Bytes, baseline.Bytes) || got.SHA256 != baseline.SHA256 {
			t.Fatalf("permutation %d changed dashboard bytes or hash", round)
		}
	}
}

// TestP213CompatibilityCorpus validates every committed Grafana Schema 41
// compatibility fixture offline, then proves canonical render idempotence.
func TestP213CompatibilityCorpus(t *testing.T) {
	corpusPath := filepath.Join("..", "..", "testdata", "dashboard", "corpus")
	entries, err := os.ReadDir(corpusPath)
	if err != nil {
		t.Fatalf("read compatibility corpus: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("compatibility corpus is empty")
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(corpusPath, entry.Name()))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			dashboardModel, err := model.Decode(data)
			if err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			if violations := model.Validate(dashboardModel); len(violations) > 0 {
				t.Fatalf("semantic validation: %v", violations)
			}
			first, err := model.Render(dashboardModel)
			if err != nil {
				t.Fatalf("canonical render: %v", err)
			}
			second, err := model.Render(dashboardModel)
			if err != nil {
				t.Fatalf("second render: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Fatal("compatibility fixture render is not deterministic")
			}
		})
	}
}

// TestP213IRFixtureInventory ensures Dashboard quality coverage consumes the
// canonical Phase 1 IR fixtures instead of maintaining a second IR format.
func TestP213IRFixtureInventory(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		mustPlan  bool
		mustBuild bool
	}{
		{name: "composite.json", mustPlan: true, mustBuild: true},
		{name: "dynamic-targets.json", mustPlan: true, mustBuild: true},
		{name: "naming-collisions.json", mustPlan: true, mustBuild: true},
		{name: "sensitive-values.json", mustPlan: true, mustBuild: true},
		{name: "invalid-references.json", mustPlan: false, mustBuild: false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "generator", "ir", fixture.name)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			document := new(observabilityv1.ObservabilityDocument)
			if err := protojson.Unmarshal(contents, document); err != nil {
				t.Fatalf("protojson: %v", err)
			}
			plan, err := p213PlanDocument(t, document)
			if fixture.mustPlan && err != nil {
				t.Fatalf("plan: %v", err)
			}
			if !fixture.mustPlan {
				if err == nil {
					t.Fatal("invalid fixture unexpectedly planned")
				}
				return
			}
			resolved, err := dashboard.Resolve(nil, nil)
			if err != nil {
				t.Fatalf("policy: %v", err)
			}
			catalog, err := dashboard.BuildCatalog(document, plan, *resolved)
			if fixture.mustBuild && err != nil {
				t.Fatalf("catalog: %v", err)
			}
			if fixture.mustBuild && len(catalog.Items) == 0 {
				t.Fatal("catalog has no items")
			}
		})
	}
}

func p213Render(t *testing.T, catalog *dashboard.DashboardCatalog, policy dashboard.DashboardPolicy) *Result {
	t.Helper()
	plan, err := Build(catalog, policy)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	result, err := Render(plan)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return result
}

func p213NoTraceLinksPolicy(t *testing.T) dashboard.DashboardPolicy {
	t.Helper()
	includeTraceLinks := false
	policy, err := dashboard.Resolve(&dashboard.DashboardConfig{IncludeTraceLinks: &includeTraceLinks}, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return *policy
}

func p213NoClientDependenciesPolicy(t *testing.T) dashboard.DashboardPolicy {
	t.Helper()
	includeClients := false
	policy, err := dashboard.Resolve(&dashboard.DashboardConfig{IncludeClientDependencies: &includeClients}, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return *policy
}

func p213DegradeCatalog(catalog *dashboard.DashboardCatalog) {
	for itemIndex := range catalog.Items {
		metrics := catalog.Items[itemIndex].Metrics
		filtered := metrics[:0]
		for _, metric := range metrics {
			if metric.Type == "counter" {
				filtered = append(filtered, metric)
			}
		}
		catalog.Items[itemIndex].Metrics = filtered
		catalog.Items[itemIndex].Spans = nil
		catalog.Items[itemIndex].Capabilities = dashboard.Capabilities{}
		// filtered holds only counter metrics; a capability is available
		// when any surviving counter carries the required attributes.
		rateAvailable, errorRatioAvailable := false, false
		for _, metric := range filtered {
			rateAvailable = rateAvailable || hasP213Attributes(metric.Attributes, "service", "operation")
			errorRatioAvailable = errorRatioAvailable || hasP213Attributes(metric.Attributes, "status")
		}
		catalog.Items[itemIndex].Capabilities.Rate = dashboard.QueryCapability{Available: rateAvailable}
		catalog.Items[itemIndex].Capabilities.ErrorRatio = dashboard.QueryCapability{Available: errorRatioAvailable}
	}
}

func hasP213Attributes(attributes []string, wanted ...string) bool {
	for _, name := range wanted {
		found := false
		for _, attribute := range attributes {
			if attribute == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func p213PermuteItems(items []dashboard.DashboardItem, round int) {
	if len(items) < 2 {
		return
	}
	shift := round % len(items)
	rotated := append([]dashboard.DashboardItem(nil), items[shift:]...)
	rotated = append(rotated, items[:shift]...)
	if round%2 == 0 {
		for left, right := 0, len(rotated)-1; left < right; left, right = left+1, right-1 {
			rotated[left], rotated[right] = rotated[right], rotated[left]
		}
	}
	copy(items, rotated)
}

package dashboard

import "testing"

// TestDigestIgnoresOutputDir verifies two semantically identical
// configurations with different output directories produce the same digest
// (AC4).
func TestDigestIgnoresOutputDir(t *testing.T) {
	left, err := Resolve(&DashboardConfig{OutputDir: str("a")}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	right, err := Resolve(&DashboardConfig{OutputDir: str("b")}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if left.Digest() != right.Digest() {
		t.Errorf("digest must ignore output_dir: %s != %s", left.Digest(), right.Digest())
	}
}

// TestDigestChangesWithContentFields verifies any field that affects
// generated JSON content changes the digest.
func TestDigestChangesWithContentFields(t *testing.T) {
	baseline, err := Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	baseDigest := baseline.Digest()

	// datasource_variable_name and rate_interval are v1-fixed to a single
	// value, so no valid resolved policy can differ on them; they cannot
	// change the digest by construction.
	variants := []struct {
		name   string
		config *DashboardConfig
	}{
		{name: "title_suffix", config: &DashboardConfig{TitleSuffix: str("Other")}},
		{name: "include_trace_links", config: &DashboardConfig{IncludeTraceLinks: boolp(false)}},
		{name: "include_client_dependencies", config: &DashboardConfig{IncludeClientDependencies: boolp(false)}},
		{name: "timezone", config: &DashboardConfig{Timezone: str("utc")}},
		{name: "refresh", config: &DashboardConfig{Refresh: str("1m")}},
		{name: "max_panels", config: &DashboardConfig{MaxPanels: intp(300)}},
		{name: "max_queries", config: &DashboardConfig{MaxQueries: intp(600)}},
		{name: "strict", config: &DashboardConfig{Strict: boolp(true)}},
	}
	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			resolved, err := Resolve(variant.config, nil)
			if err != nil {
				t.Fatalf("Resolve failed: %v", err)
			}
			if resolved.Digest() == baseDigest {
				t.Errorf("digest unchanged for %s", variant.name)
			}
		})
	}
}

// TestDigestStableAcrossRuns verifies the digest is deterministic for the
// same policy.
func TestDigestStableAcrossRuns(t *testing.T) {
	policy, err := Resolve(nil, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	first := policy.Digest()
	for index := 0; index < 9; index++ {
		if digest := policy.Digest(); digest != first {
			t.Fatalf("digest changed between runs: %s != %s", digest, first)
		}
	}
}

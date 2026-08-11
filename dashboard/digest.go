package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Digest returns a deterministic fingerprint of the dashboard policy
// contents using canonical JSON (fixed struct field order) over a SHA-256
// hash. The digest excludes OutputDir on purpose: the same semantic
// configuration must produce the same digest regardless of the working
// directory or output path. Every field that affects generated JSON
// content participates in the digest.
func (policy *DashboardPolicy) Digest() string {
	payload := digestPayload{
		TitleSuffix:               policy.TitleSuffix,
		DatasourceVariableName:    policy.DatasourceVariableName,
		IncludeTraceLinks:         policy.IncludeTraceLinks,
		IncludeClientDependencies: policy.IncludeClientDependencies,
		RateInterval:              policy.RateInterval,
		Timezone:                  policy.Timezone,
		Refresh:                   policy.Refresh,
		MaxPanels:                 policy.MaxPanels,
		MaxQueries:                policy.MaxQueries,
		Strict:                    policy.Strict,
	}
	// Field order of the payload struct is the canonical JSON order;
	// encoding/json never reorders struct fields, so the bytes are stable.
	contents, err := json.Marshal(payload)
	if err != nil {
		// The payload contains only strings, bools and ints; marshaling
		// cannot fail for any policy this package builds.
		panic("dashboard policy digest marshal failed: " + err.Error())
	}
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

type digestPayload struct {
	TitleSuffix               string `json:"title_suffix"`
	DatasourceVariableName    string `json:"datasource_variable_name"`
	IncludeTraceLinks         bool   `json:"include_trace_links"`
	IncludeClientDependencies bool   `json:"include_client_dependencies"`
	RateInterval              string `json:"rate_interval"`
	Timezone                  string `json:"timezone"`
	Refresh                   string `json:"refresh"`
	MaxPanels                 int64  `json:"max_panels"`
	MaxQueries                int64  `json:"max_queries"`
	Strict                    bool   `json:"strict"`
}

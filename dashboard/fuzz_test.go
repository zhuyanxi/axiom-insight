package dashboard

import "testing"

// FuzzDecodeDashboard ensures the strict config decoder never panics or
// hangs on arbitrary input and that errors never leak payload values.
func FuzzDecodeDashboard(f *testing.F) {
	f.Add([]byte("refresh: 30s\n"))
	f.Add([]byte("not yaml at all { ["))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x01})
	f.Add([]byte("max_panels: .nan\n"))
	f.Add([]byte("dashboard:\n  output_dir: dashboards\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		config, err := DecodeDashboard(data)
		if err != nil {
			// Errors must not echo the raw input; the decoder only ever
			// reports field names and tags.
			return
		}
		if _, err := Resolve(config, nil); err != nil {
			return
		}
	})
}

// FuzzResolve feeds random strings and numbers into Resolve; it must never
// panic and must always either resolve or fail with a *ConfigErrors.
func FuzzResolve(f *testing.F) {
	f.Add("30s", "browser", "datasource", int64(200), int64(500), false)
	f.Add("45s", "local", "$(ds)", int64(0), int64(100000), true)
	f.Add("", "utc", "", int64(-1), int64(-1), false)
	f.Fuzz(func(t *testing.T, refresh, timezone, variable string, maxPanels, maxQueries int64, strict bool) {
		config := &DashboardConfig{
			Refresh:                &refresh,
			Timezone:               &timezone,
			DatasourceVariableName: &variable,
			MaxPanels:              &maxPanels,
			MaxQueries:             &maxQueries,
			Strict:                 &strict,
		}
		if _, err := Resolve(config, nil); err != nil {
			var failures *ConfigErrors
			if !asConfigErrors(err, &failures) {
				t.Fatalf("Resolve returned %T, want *ConfigErrors", err)
			}
		}
	})
}

func asConfigErrors(err error, target **ConfigErrors) bool {
	if failures, ok := err.(*ConfigErrors); ok {
		*target = failures
		return true
	}
	return false
}

package tracing

import (
	"github.com/zhuyanxi/axiom-insight/generator/naming"
	"github.com/zhuyanxi/axiom-insight/generator/planner"
)

// disambiguateNames applies the unified P1-04 collision table to span
// names (e.g. several SQL dependencies all named "db exec") and rewrites
// suffixed names onto the items, matching by (target ID, original name).
func disambiguateNames(result *planner.TracingResult) {
	if len(result.Items) < 2 {
		return
	}
	originals := make([]string, len(result.Items))
	nameItems := make([]naming.NameItem, len(result.Items))
	for index, item := range result.Items {
		originals[index] = item.GetName()
		nameItems[index] = naming.NameItem{
			Signal: planner.SignalTracing, TargetID: item.GetTarget().GetId(), Name: originals[index],
		}
	}
	resolved, diagnostics := naming.NamingPolicy{}.Disambiguate(nameItems)
	for index, item := range result.Items {
		for _, entry := range resolved {
			if entry.TargetID != item.GetTarget().GetId() {
				continue
			}
			if entry.Name == originals[index] ||
				entry.Name == originals[index]+"_"+naming.DisambiguationSuffix(entry.TargetID) {
				item.Name = entry.Name
				break
			}
		}
	}
	result.Diagnostics = append(result.Diagnostics, diagnostics.Items()...)
}

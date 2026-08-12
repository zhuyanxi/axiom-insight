package overview

import (
	"slices"
	"sort"
	"strings"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

// metricFamily is one metric semantic family: items whose MetricPlans
// declare the same controlled name, type and label schema. Overview
// queries aggregate a family and never merge different names or label
// schemas into one selector.
type metricFamily struct {
	// Key is the deterministic family identity.
	Key string
	// Name is the exact declared MetricPlan name.
	Name string
	// Type is counter, histogram or gauge.
	Type string
	// Unit is the plan-declared metric unit.
	Unit string
	// Attributes are the sorted controlled label keys.
	Attributes []string
	// PlanIDs, ItemIDs and Categories are sorted and deduplicated.
	PlanIDs    []string
	ItemIDs    []string
	Categories []dashboard.Category
	// Items are the catalog items declaring this family, sorted by ID.
	Items []dashboard.DashboardItem
	// Capabilities gate the overview panels this family can feed.
	Capabilities familyCapabilities
}

type familyCapabilities struct {
	Rate        bool
	ErrorRatio  bool
	Percentiles bool
	InFlight    bool
}

// buildFamilies groups the catalog's metric references into sorted
// families. Metrics whose names fail the controlled gate are dropped
// with DASHBOARD_SENSITIVE_VALUE_DROPPED diagnostics; they never enter a
// selector.
func buildFamilies(catalog *dashboard.DashboardCatalog, diagnostics *[]dashboard.Diagnostic) []*metricFamily {
	byKey := make(map[string]*metricFamily)
	for _, item := range catalog.Items {
		for _, metric := range item.Metrics {
			if !query.ValidMetricName(metric.Name) {
				*diagnostics = append(*diagnostics, dashboard.Diagnostic{
					Code: dashboard.CodeSensitiveValueDropped, TargetID: item.ID,
					Field: "metrics[].name", Message: "metric name is not a controlled machine name; the metric was dropped",
				})
				continue
			}
			attributes := append([]string(nil), metric.Attributes...)
			sort.Strings(attributes)
			key := familyKey(metric.Name, metric.Type, attributes)
			family := byKey[key]
			if family == nil {
				family = &metricFamily{
					Key: key, Name: metric.Name, Type: metric.Type, Unit: metric.Unit,
					Attributes:   attributes,
					Capabilities: familyCapabilitiesFrom(metric.Type, attributes),
				}
				byKey[key] = family
			}
			family.PlanIDs = appendUniqueString(family.PlanIDs, metric.PlanID)
			family.ItemIDs = appendUniqueString(family.ItemIDs, item.ID)
			family.Categories = appendUniqueCategory(family.Categories, item.Category)
			family.Items = appendUniqueItem(family.Items, item)
		}
	}

	families := make([]*metricFamily, 0, len(byKey))
	for _, family := range byKey {
		families = append(families, family)
	}
	sort.Slice(families, func(left, right int) bool { return families[left].Key < families[right].Key })
	for _, family := range families {
		sort.Strings(family.PlanIDs)
		sort.Strings(family.ItemIDs)
		sort.Slice(family.Categories, func(left, right int) bool {
			return family.Categories[left] < family.Categories[right]
		})
		sort.Slice(family.Items, func(left, right int) bool {
			return family.Items[left].ID < family.Items[right].ID
		})
	}
	return families
}

// familyKey is the stable family identity: name + type + sorted label
// schema. The NUL separators keep ("a", "b c") and ("a b", "c") apart.
func familyKey(name, metricType string, attributes []string) string {
	return name + "\x00" + metricType + "\x00" + strings.Join(attributes, ",")
}

// familyCapabilitiesFrom derives the overview capabilities from the
// plan-declared metric schema. Overview queries scope by service, so
// every capability also requires the service label; rate and in-flight
// additionally require operation where the query groups by it.
func familyCapabilitiesFrom(metricType string, attributes []string) familyCapabilities {
	has := func(label string) bool { return slices.Contains(attributes, label) }
	return familyCapabilities{
		Rate:        metricType == "counter" && has("service") && has("operation"),
		ErrorRatio:  metricType == "counter" && has("service") && has("status"),
		Percentiles: metricType == "histogram" && has("service"),
		InFlight:    metricType == "gauge" && has("service"),
	}
}

func hasAttribute(family *metricFamily, label string) bool {
	return slices.Contains(family.Attributes, label)
}

func appendUniqueString(values []string, wanted string) []string {
	if slices.Contains(values, wanted) {
		return values
	}
	return append(values, wanted)
}

func appendUniqueCategory(values []dashboard.Category, wanted dashboard.Category) []dashboard.Category {
	if slices.Contains(values, wanted) {
		return values
	}
	return append(values, wanted)
}

func appendUniqueItem(values []dashboard.DashboardItem, wanted dashboard.DashboardItem) []dashboard.DashboardItem {
	for _, item := range values {
		if item.ID == wanted.ID {
			return values
		}
	}
	return append(values, wanted)
}

package category

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

type panelSpec struct {
	title       string
	description string
	panelType   string
	width       int
	height      int
	unit        string
	noValue     string
	legend      string
}

// specForPurpose returns the fixed panel shape for one P2-05 purpose.
func specForPurpose(purpose string) panelSpec {
	switch purpose {
	case "rate":
		return panelSpec{
			title:       "request rate",
			description: "Request rate of this endpoint; requires runtime instrumentation from the Phase 1 Instrumentation Plan.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	case "error_ratio":
		return panelSpec{
			title:       "error ratio",
			description: "Share of requests ending in the fixed error status pattern (5xx or error); requires runtime instrumentation.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "percent", noValue: "0", legend: "{{operation}}",
		}
	case "p50":
		return panelSpec{
			title:       "p50 duration",
			description: "Median request duration in seconds (histogram_quantile 0.50); requires runtime instrumentation.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p95":
		return panelSpec{
			title:       "p95 duration",
			description: "95th percentile request duration in seconds; requires runtime instrumentation.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "p99":
		return panelSpec{
			title:       "p99 duration",
			description: "99th percentile request duration in seconds; requires runtime instrumentation.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "s", noValue: "0", legend: "{{operation}}",
		}
	case "in_flight":
		return panelSpec{
			title:       "in-flight",
			description: "Number of requests currently in flight for this endpoint; requires runtime instrumentation.",
			panelType:   model.PanelTypeStat, width: dashboard.PanelWidthStat, height: 8,
			unit: "short", noValue: "0", legend: "{{operation}}",
		}
	default:
		return panelSpec{
			title:       "operations",
			description: "Request rate per operation across the counter metric families of this category; requires runtime instrumentation.",
			panelType:   model.PanelTypeTable, width: dashboard.PanelWidthTable, height: 8,
			unit: "ops/s", noValue: "0", legend: "{{operation}}",
		}
	}
}

// operationFamily is one counter metric family feeding the operation
// breakdown table: same metric name, type and label schema.
type operationFamily struct {
	key        string
	metric     dashboard.SignalReference
	planIDs    []string
	operations []string
	itemIDs    []string
}

// operationTable builds the per-category operation breakdown table: one
// query per controlled operation value of every counter family that
// declares service and operation. Different metric name or label schemas
// are never merged into one selector; each family keeps its own targets.
func operationTable(items []dashboard.DashboardItem, category dashboard.Category, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) (Panel, error) {
	targets, err := operationTableTargets(items, serviceName, policy, diagnostics)
	if err != nil {
		return Panel{}, err
	}
	if len(targets) > model.MaxTargetsPerPanel {
		return Panel{}, &dashboard.CatalogError{
			Code:    dashboard.CodePanelLimitExceeded,
			Field:   "rows." + string(category) + ".operations.targets",
			Message: fmt.Sprintf("operation table exceeds the fixed limit of %d targets", model.MaxTargetsPerPanel),
		}
	}
	spec := specForPurpose("operations")
	table := Panel{
		Key:         dashboard.PanelIDKey(category, operationTableItemID, "operations"),
		ItemID:      operationTableItemID,
		Purpose:     "operations",
		Title:       dashboard.PanelTitle(category, "", "operations"),
		Description: spec.description,
		Type:        spec.panelType,
		Width:       spec.width,
		Height:      spec.height,
		Unit:        spec.unit,
		NoValue:     spec.noValue,
		Targets:     targets,
	}
	return table, nil
}

// operationTableTargets builds the per-operation targets shared by the
// HTTP/RPC and Kafka operation tables: one query per controlled operation
// value of every counter family that declares service and operation.
// Different metric name or label schemas are never merged into one
// selector; each family keeps its own targets.
func operationTableTargets(items []dashboard.DashboardItem, serviceName string, policy dashboard.DashboardPolicy, diagnostics *[]dashboard.Diagnostic) ([]Target, error) {
	families := operationFamilies(items, diagnostics)
	var targets []Target
	for _, family := range families {
		plans, planDiagnostics, err := query.PlanOperationBreakdown(family.metric, family.operations, serviceName, policy)
		if err != nil {
			return nil, err
		}
		*diagnostics = append(*diagnostics, planDiagnostics...)
		for _, plan := range plans {
			expression, err := query.Render(plan.Expression)
			if err != nil {
				return nil, &dashboard.CatalogError{
					Code:    dashboard.CodeRenderError,
					Field:   "queries[" + plan.CanonicalKey + "]",
					Message: err.Error(),
				}
			}
			if _, err := query.Parse(expression); err != nil {
				return nil, &dashboard.CatalogError{
					Code:    dashboard.CodeRenderError,
					Field:   "queries[" + plan.CanonicalKey + "]",
					Message: fmt.Sprintf("operation query is outside the supported PromQL subset: %v", err),
				}
			}
			planID := ""
			if len(plan.PlanIDs) > 0 {
				planID = plan.PlanIDs[0]
			}
			targets = append(targets, Target{
				CanonicalKey: plan.CanonicalKey,
				Kind:         string(plan.Kind),
				PlanID:       planID,
				TargetID:     family.itemIDs[0],
				Expr:         expression,
				LegendFormat: "{{operation}}",
			})
		}
	}
	return targets, nil
}

func operationFamilies(items []dashboard.DashboardItem, diagnostics *[]dashboard.Diagnostic) []operationFamily {
	byKey := make(map[string]*operationFamily)
	for _, item := range items {
		for _, metric := range item.Metrics {
			if metric.Type != "counter" || !query.ValidMetricName(metric.Name) {
				continue
			}
			if !hasAttributes(metric, "service", "operation") {
				continue
			}
			attributes := append([]string(nil), metric.Attributes...)
			sort.Strings(attributes)
			key := metric.Name + "\x00" + strings.Join(attributes, ",")
			family := byKey[key]
			if family == nil {
				family = &operationFamily{
					key: key,
					metric: dashboard.SignalReference{
						PlanID: metric.PlanID, Name: metric.Name, Type: metric.Type,
						Unit: metric.Unit, Attributes: attributes,
					},
					planIDs: []string{metric.PlanID},
				}
				byKey[key] = family
			} else if !slices.Contains(family.planIDs, metric.PlanID) {
				family.planIDs = append(family.planIDs, metric.PlanID)
			}
			if !slices.Contains(family.operations, item.Operation) {
				family.operations = append(family.operations, item.Operation)
			}
			if !slices.Contains(family.itemIDs, item.ID) {
				family.itemIDs = append(family.itemIDs, item.ID)
			}
		}
	}
	families := make([]operationFamily, 0, len(byKey))
	for _, family := range byKey {
		sort.Strings(family.planIDs)
		sort.Strings(family.operations)
		sort.Strings(family.itemIDs)
		// Deterministic representative: the smallest plan ID, so the
		// table's canonical keys and metadata never depend on input order.
		family.metric.PlanID = family.planIDs[0]
		families = append(families, *family)
	}
	sort.Slice(families, func(left, right int) bool { return families[left].key < families[right].key })
	return families
}

func hasAttributes(metric dashboard.SignalReference, wanted ...string) bool {
	for _, attribute := range wanted {
		if !slices.Contains(metric.Attributes, attribute) {
			return false
		}
	}
	return true
}

// traceLinkURL builds the controlled internal trace link: the reserved
// datasource variable plus URL-escaped validated service, operation and
// span names. Trace/request IDs, hosts and dynamic parameters never
// appear.
func traceLinkURL(link query.TraceLinkPlan) string {
	queryPart := "var-datasource=${" + link.DatasourceVariable + "}" +
		"&var-service=" + url.QueryEscape(link.ServiceName) +
		"&var-operation=" + url.QueryEscape(link.Operation) +
		"&var-span=" + url.QueryEscape(link.SpanName)
	return "/d/${uid}?" + queryPart
}

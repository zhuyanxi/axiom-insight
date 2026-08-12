package overview

import (
	"sort"

	"github.com/zhuyanxi/axiom-insight/dashboard"
	"github.com/zhuyanxi/axiom-insight/dashboard/model"
	"github.com/zhuyanxi/axiom-insight/dashboard/query"
)

// datasourceVariable is the single reserved datasource variable: name from
// the policy (v1 allows only "datasource"), type datasource, query
// "prometheus", hide 0. It never carries a URL, UID or credential.
func datasourceVariable(policy dashboard.DashboardPolicy) model.Variable {
	hide := 0
	return model.Variable{
		Name:  policy.DatasourceVariableName,
		Type:  model.VariableTypeDatasource,
		Query: "prometheus",
		Hide:  &hide,
	}
}

// operationVariable builds the controlled operation variable when the
// catalog declares at least two valid low-cardinality operations.
// Candidates come from the validated normalized operations in canonical
// order as static custom options; label_values and raw user values never
// enter. Invalid operations are dropped with diagnostics.
func operationVariable(items []dashboard.DashboardItem, diagnostics *[]dashboard.Diagnostic) *model.Variable {
	seen := make(map[string]bool, len(items))
	var operations []string
	for _, item := range items {
		if !query.ValidOperationValue(item.Operation) {
			*diagnostics = append(*diagnostics, dashboard.Diagnostic{
				Code: dashboard.CodeSensitiveValueDropped, TargetID: item.ID,
				Field: "operation", Message: "operation value is not a controlled value; it was dropped",
			})
			continue
		}
		if !seen[item.Operation] {
			seen[item.Operation] = true
			operations = append(operations, item.Operation)
		}
	}
	sort.Strings(operations)
	if len(operations) < 2 {
		return nil
	}
	options := make([]model.VariableOption, 0, len(operations))
	for _, operation := range operations {
		options = append(options, model.VariableOption{Text: operation, Value: operation})
	}
	return &model.Variable{
		Name:       "operation",
		Type:       model.VariableTypeCustom,
		Options:    options,
		Current:    &model.VariableCurrent{Text: operations[0], Value: operations[0]},
		Multi:      true,
		IncludeAll: true,
	}
}

package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Render serializes a typed expression into the supported PromQL subset.
// The renderer walks the typed tree and quotes every value with
// strconv.Quote; external content never reaches the output unescaped.
// Rendering is a pure function: identical trees yield identical bytes.
func Render(expression Expression) (string, error) {
	if expression == nil {
		return "", fmt.Errorf("query: render: expression is nil")
	}
	var builder strings.Builder
	if err := renderExpression(&builder, expression); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func renderExpression(builder *strings.Builder, expression Expression) error {
	switch node := expression.(type) {
	case *MetricSelector:
		return renderSelector(builder, node)
	case *RateExpression:
		builder.WriteString("rate(")
		if err := renderSelector(builder, node.Selector); err != nil {
			return err
		}
		builder.WriteString("[" + node.Interval + "])")
		return nil
	case *Aggregation:
		builder.WriteString("sum")
		if len(node.By) > 0 {
			builder.WriteString(" by (")
			for index, label := range node.By {
				if index > 0 {
					builder.WriteString(", ")
				}
				builder.WriteString(label)
			}
			builder.WriteString(")")
		}
		builder.WriteString(" (")
		if err := renderExpression(builder, node.Expr); err != nil {
			return err
		}
		builder.WriteString(")")
		return nil
	case *HistogramQuantileExpression:
		builder.WriteString("histogram_quantile(")
		builder.WriteString(strconv.FormatFloat(node.Quantile, 'g', -1, 64))
		builder.WriteString(", ")
		if err := renderExpression(builder, node.Expr); err != nil {
			return err
		}
		builder.WriteString(")")
		return nil
	case *BinaryExpression:
		builder.WriteString("(")
		if err := renderExpression(builder, node.Left); err != nil {
			return err
		}
		builder.WriteString(" / ")
		if err := renderExpression(builder, node.Right); err != nil {
			return err
		}
		builder.WriteString(")")
		return nil
	case *ScalarExpression:
		builder.WriteString(strconv.FormatFloat(node.Value, 'g', -1, 64))
		return nil
	default:
		return fmt.Errorf("query: render: unsupported expression node %T", expression)
	}
}

func renderSelector(builder *strings.Builder, selector *MetricSelector) error {
	if selector == nil || selector.MetricName == "" {
		return fmt.Errorf("query: render: selector without a metric name")
	}
	builder.WriteString(selector.MetricName)
	if len(selector.Matchers) == 0 {
		return nil
	}
	builder.WriteString("{")
	for index, matcher := range selector.Matchers {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(matcher.Label)
		switch matcher.Op {
		case MatchEqual:
			builder.WriteString("=")
		case MatchNotEqual:
			builder.WriteString("!=")
		case MatchRegex:
			builder.WriteString("=~")
		default:
			return fmt.Errorf("query: render: unsupported matcher operator %d", matcher.Op)
		}
		builder.WriteString(strconv.Quote(matcher.Value))
	}
	builder.WriteString("}")
	return nil
}

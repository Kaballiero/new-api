package billingexpr

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"slices"
)

func CatalogPrices(expression string) [][]DisplayPrice {
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return nil
	}
	var nodes []ast.Node
	pending := []ast.Node{tree.Node}
	for len(pending) > 0 {
		if len(pending)+len(nodes) > 64 {
			return nil
		}
		node := pending[0]
		pending = pending[1:]
		if conditional, ok := node.(*ast.ConditionalNode); ok {
			condition, err := lowerCondition(conditional.Cond, getCompileEnv(DefaultExprVersion))
			if err != nil || (condition.Var != "len" && condition.Var != "p" && condition.Var != "c") {
				return nil
			}
			pending = append(pending, conditional.Exp1, conditional.Exp2)
		} else {
			nodes = append(nodes, node)
		}
	}
	result := make([][]DisplayPrice, 0, len(nodes))
	for _, node := range nodes {
		if call, ok := node.(*ast.CallNode); ok && isTierCall(call) {
			node = call.Arguments[1]
		}
		prices := make(map[string]DisplayPrice)
		if !displayPriceTerms(node, prices) || len(prices) == 0 {
			return nil
		}
		keys := make([]string, 0, len(prices))
		for key := range prices {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		row := make([]DisplayPrice, 0, len(keys))
		for _, key := range keys {
			row = append(row, prices[key])
		}
		result = append(result, row)
	}
	return result
}

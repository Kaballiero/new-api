package billingexpr

import (
	"math"
	"slices"

	"github.com/expr-lang/expr/ast"
)

type DisplayPrice struct {
	Component string
	Unit      string
	AmountUSD float64
}

func displayPriceTerms(node ast.Node, prices map[string]DisplayPrice) bool {
	if sum, ok := node.(*ast.BinaryNode); ok && sum.Operator == "+" {
		return displayPriceTerms(sum.Left, prices) && displayPriceTerms(sum.Right, prices)
	}
	var price DisplayPrice
	if call, ok := node.(*ast.CallNode); ok && displayCallName(call) == "fixed" && len(call.Arguments) == 1 {
		amount, ok := requestRuleNumber(call.Arguments[0])
		if !ok || len(prices) != 0 {
			return false
		}
		price = DisplayPrice{Component: "base", Unit: "call", AmountUSD: amount}
	} else {
		variable, coefficient, ok := linearTerm(node, getCompileEnv(DefaultExprVersion))
		if !ok || !slices.Contains([]string{"p", "c", "cr", "cc", "cc1h", "img", "img_o", "ai", "ao"}, variable) {
			return false
		}
		price = DisplayPrice{Component: variable, Unit: "million_tokens", AmountUSD: coefficient}
	}
	if _, duplicate := prices[price.Component]; duplicate || price.AmountUSD < 0 || math.IsNaN(price.AmountUSD) || math.IsInf(price.AmountUSD, 0) {
		return false
	}
	if len(prices) > 0 {
		if _, fixed := prices["base"]; fixed || price.Component == "base" {
			return false
		}
	}
	prices[price.Component] = price
	return true
}

func displayCallName(call *ast.CallNode) string {
	identifier, ok := call.Callee.(*ast.IdentifierNode)
	if !ok {
		return ""
	}
	return identifier.Value
}

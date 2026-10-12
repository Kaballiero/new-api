package billingexpr

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

type DisplayUsageField struct {
	Unit string
	Enum []string
}

type DisplayUsageGuard struct {
	Values  map[string]string `json:"values"`
	Negated bool              `json:"negated"`
}

type DisplayPrice struct {
	Component string
	Unit      string
	AmountUSD float64
}

type DisplayTier struct {
	Label       string
	Conditions  []TierCondition
	UsageGuards []DisplayUsageGuard
	Prices      []DisplayPrice
}

func EnumerateDisplayTiers(expression string, usageSchema map[string]DisplayUsageField) ([]DisplayTier, error) {
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return nil, err
	}
	if ast.Find(tree.Node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.IdentifierNode)
		return ok && identifier.Value == "fixed"
	}) != nil {
		if err := validateFixedPricingTree(tree.Node); err != nil {
			return nil, err
		}
	}
	return collectDisplayTiers(tree.Node, nil, nil, usageSchema)
}

func collectDisplayTiers(node ast.Node, conditions []TierCondition, usageGuards []DisplayUsageGuard, schema map[string]DisplayUsageField) ([]DisplayTier, error) {
	if conditional, ok := node.(*ast.ConditionalNode); ok {
		var thenConditions, elseConditions []TierCondition
		var thenUsage, elseUsage []DisplayUsageGuard
		if len(schema) > 0 {
			values := make(map[string]string)
			if !displayUsageValues(conditional.Cond, schema, values) {
				return nil, fmt.Errorf("unsupported usage condition")
			}
			thenUsage = append(usageGuards[:len(usageGuards):len(usageGuards)], DisplayUsageGuard{Values: values})
			elseUsage = append(usageGuards[:len(usageGuards):len(usageGuards)], DisplayUsageGuard{Values: values, Negated: true})
		} else {
			condition, err := lowerCondition(conditional.Cond, getCompileEnv(DefaultExprVersion))
			if err != nil {
				return nil, err
			}
			thenConditions = appendGuard(conditions, condition)
			condition.Negated = !condition.Negated
			elseConditions = appendGuard(conditions, condition)
		}
		then, err := collectDisplayTiers(conditional.Exp1, thenConditions, thenUsage, schema)
		if err != nil {
			return nil, err
		}
		otherwise, err := collectDisplayTiers(conditional.Exp2, elseConditions, elseUsage, schema)
		if err != nil {
			return nil, err
		}
		return append(then, otherwise...), nil
	}
	result := DisplayTier{Conditions: conditions, UsageGuards: usageGuards}
	if call, ok := node.(*ast.CallNode); ok && isTierCall(call) {
		label, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			return nil, fmt.Errorf("unsupported tier label")
		}
		result.Label, node = label.Value, call.Arguments[1]
	}
	prices := make(map[string]DisplayPrice)
	if !displayPriceTerms(node, schema, prices) || len(prices) == 0 {
		return nil, fmt.Errorf("unsupported display price")
	}
	keys := make([]string, 0, len(prices))
	for key := range prices {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Prices = append(result.Prices, prices[key])
	}
	return []DisplayTier{result}, nil
}

func displayPriceTerms(node ast.Node, schema map[string]DisplayUsageField, prices map[string]DisplayPrice) bool {
	if sum, ok := node.(*ast.BinaryNode); ok && sum.Operator == "+" {
		return displayPriceTerms(sum.Left, schema, prices) && displayPriceTerms(sum.Right, schema, prices)
	}
	var price DisplayPrice
	if len(schema) > 0 {
		if amount, ok := requestRuleNumber(node); ok {
			price = DisplayPrice{Component: "base", Unit: "call", AmountUSD: amount}
		} else {
			var ok bool
			price, ok = displayUsagePrice(node, schema)
			if !ok {
				return false
			}
		}
	} else if call, ok := node.(*ast.CallNode); ok && displayCallName(call) == "fixed" && len(call.Arguments) == 1 {
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
	if _, duplicate := prices[price.Component]; duplicate || !displayAmountValid(price.AmountUSD) {
		return false
	}
	if len(schema) == 0 && len(prices) > 0 {
		if _, fixed := prices["base"]; fixed || price.Component == "base" {
			return false
		}
	}
	prices[price.Component] = price
	return true
}

func displayUsagePrice(node ast.Node, schema map[string]DisplayUsageField) (DisplayPrice, bool) {
	divided := false
	if division, ok := node.(*ast.BinaryNode); ok && division.Operator == "/" {
		divisor, ok := requestRuleNumber(division.Right)
		if !ok || divisor != 1_000_000 {
			return DisplayPrice{}, false
		}
		node, divided = division.Left, true
	}
	product, ok := node.(*ast.BinaryNode)
	if !ok || product.Operator != "*" {
		return DisplayPrice{}, false
	}
	key, ok := displayUsageKey(product.Left)
	amount, numberOK := requestRuleNumber(product.Right)
	if !ok || !numberOK {
		return DisplayPrice{}, false
	}
	field, declared := schema[key]
	if !declared || len(field.Enum) > 0 {
		return DisplayPrice{}, false
	}
	unit := field.Unit
	if unit == "token" {
		if !divided {
			return DisplayPrice{}, false
		}
		unit = "million_tokens"
	} else if divided || !slices.Contains([]string{"second", "count", "credit"}, unit) {
		return DisplayPrice{}, false
	}
	return DisplayPrice{Component: key, Unit: unit, AmountUSD: amount}, true
}

func displayUsageValues(node ast.Node, schema map[string]DisplayUsageField, values map[string]string) bool {
	comparison, ok := node.(*ast.BinaryNode)
	if !ok {
		return false
	}
	if comparison.Operator == "&&" || comparison.Operator == "and" {
		return displayUsageValues(comparison.Left, schema, values) && displayUsageValues(comparison.Right, schema, values)
	}
	if comparison.Operator != "==" {
		return false
	}
	key, ok := displayUsageKey(comparison.Left)
	value, stringOK := comparison.Right.(*ast.StringNode)
	if !ok || !stringOK || !slices.Contains(schema[key].Enum, value.Value) {
		return false
	}
	if previous, duplicate := values[key]; duplicate && previous != value.Value {
		return false
	}
	values[key] = value.Value
	return true
}

func displayUsageKey(node ast.Node) (string, bool) {
	call, ok := node.(*ast.CallNode)
	if !ok || displayCallName(call) != "u" || len(call.Arguments) != 1 {
		return "", false
	}
	key, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		return "", false
	}
	return key.Value, true
}

func displayCallName(call *ast.CallNode) string {
	identifier, ok := call.Callee.(*ast.IdentifierNode)
	if !ok {
		return ""
	}
	return identifier.Value
}

func displayAmountValid(amount float64) bool {
	return amount >= 0 && !math.IsNaN(amount) && !math.IsInf(amount, 0)
}

package billingexpr

import (
	"fmt"
	"maps"
	"slices"

	"github.com/expr-lang/expr/ast"
)

const maxTaskDisplayBranches = 256
const maxTaskDisplayDepth = 64
const maxTaskDisplayVisits = 4096

type taskDisplayBranch struct {
	prices map[string]float64
	guards []DisplayUsageGuard
}

// projectTaskDisplay expands only finite affine task prices and declared enum branches.
// It never evaluates a request or substitutes sample usage values.
func projectTaskDisplay(node ast.Node, schema map[string]DisplayUsageField) ([]DisplayTier, error) {
	label := ""
	if call, ok := node.(*ast.CallNode); ok && isTierCall(call) {
		name, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			return nil, fmt.Errorf("unsupported task display label")
		}
		label, node = name.Value, call.Arguments[1]
	}
	visits := maxTaskDisplayVisits
	branches, err := taskDisplayBranches(node, schema, 0, &visits)
	if err != nil {
		return nil, err
	}
	if len(branches) == 0 {
		return nil, fmt.Errorf("empty task display projection")
	}
	tiers := make([]DisplayTier, 0, len(branches))
	for _, branch := range branches {
		keys := slices.Sorted(maps.Keys(branch.prices))
		prices := make([]DisplayPrice, 0, len(keys))
		for _, key := range keys {
			unit := schema[key].Unit
			component := key
			if key == "" {
				component, unit = "base", "call"
			}
			prices = append(prices, DisplayPrice{Component: component, Unit: unit, AmountUSD: branch.prices[key]})
		}
		tiers = append(tiers, DisplayTier{Label: label, UsageGuards: branch.guards, Prices: prices})
	}
	return tiers, nil
}

func taskDisplayBranches(node ast.Node, schema map[string]DisplayUsageField, depth int, visits *int) ([]taskDisplayBranch, error) {
	*visits--
	if *visits < 0 {
		return nil, fmt.Errorf("task display work limit")
	}
	if depth > maxTaskDisplayDepth {
		return nil, fmt.Errorf("task display depth limit")
	}
	if value, ok := requestRuleNumber(node); ok && displayAmountValid(value) {
		return []taskDisplayBranch{{prices: map[string]float64{"": value}}}, nil
	}
	if key, ok := displayUsageKey(node); ok {
		field, declared := schema[key]
		if key == "" || key == "base" || !declared || len(field.Enum) > 0 || !slices.Contains([]string{"second", "count", "credit"}, field.Unit) {
			return nil, fmt.Errorf("unsupported task display quantity")
		}
		return []taskDisplayBranch{{prices: map[string]float64{key: 1}}}, nil
	}
	if conditional, ok := node.(*ast.ConditionalNode); ok {
		var result []taskDisplayBranch
		for _, alternative := range []struct {
			node    ast.Node
			negated bool
		}{{conditional.Exp1, false}, {conditional.Exp2, true}} {
			guards, err := taskDisplayConditions(conditional.Cond, schema, alternative.negated, depth+1, visits)
			if err != nil {
				return nil, err
			}
			branches, err := taskDisplayBranches(alternative.node, schema, depth+1, visits)
			if err != nil {
				return nil, err
			}
			for _, condition := range guards {
				for _, branch := range branches {
					*visits--
					if *visits < 0 {
						return nil, fmt.Errorf("task display work limit")
					}
					combined := append(slices.Clone(condition), branch.guards...)
					if taskDisplayGuardsPossible(combined, schema) {
						if len(result) >= maxTaskDisplayBranches {
							return nil, fmt.Errorf("task display branch limit")
						}
						result = append(result, taskDisplayBranch{prices: branch.prices, guards: combined})
					}
				}
			}
		}
		return result, nil
	}
	operator := ""
	var leftNode, rightNode ast.Node
	if binary, ok := node.(*ast.BinaryNode); ok {
		operator, leftNode, rightNode = binary.Operator, binary.Left, binary.Right
	} else if call, ok := node.(*ast.CallNode); ok && displayCallName(call) == "max" && len(call.Arguments) == 2 {
		operator, leftNode, rightNode = "max", call.Arguments[0], call.Arguments[1]
	} else if call, ok := node.(*ast.BuiltinNode); ok && call.Name == "max" && len(call.Arguments) == 2 {
		operator, leftNode, rightNode = "max", call.Arguments[0], call.Arguments[1]
	}
	if !slices.Contains([]string{"+", "*", "/", "max"}, operator) {
		return nil, fmt.Errorf("unsupported task display arithmetic")
	}
	left, err := taskDisplayBranches(leftNode, schema, depth+1, visits)
	if err != nil {
		return nil, err
	}
	right, err := taskDisplayBranches(rightNode, schema, depth+1, visits)
	if err != nil {
		return nil, err
	}
	var result []taskDisplayBranch
	for _, a := range left {
		for _, b := range right {
			*visits--
			if *visits < 0 {
				return nil, fmt.Errorf("task display work limit")
			}
			guards := append(slices.Clone(a.guards), b.guards...)
			if !taskDisplayGuardsPossible(guards, schema) {
				continue
			}
			prices, err := taskDisplayArithmetic(a.prices, b.prices, operator)
			if err != nil {
				return nil, err
			}
			if len(result) >= maxTaskDisplayBranches {
				return nil, fmt.Errorf("task display branch limit")
			}
			result = append(result, taskDisplayBranch{prices: prices, guards: guards})
		}
	}
	return result, nil
}

func taskDisplayArithmetic(left, right map[string]float64, operator string) (map[string]float64, error) {
	result := maps.Clone(left)
	leftConstant, leftFixed := left[""]
	rightConstant, rightFixed := right[""]
	leftFixed = leftFixed && len(left) == 1
	rightFixed = rightFixed && len(right) == 1
	switch operator {
	case "+":
		for key, amount := range right {
			result[key] += amount
		}
	case "*":
		if leftFixed {
			result, rightConstant, rightFixed = maps.Clone(right), leftConstant, true
		}
		if !rightFixed {
			return nil, fmt.Errorf("nonlinear task display price")
		}
		for key := range result {
			result[key] *= rightConstant
		}
	case "/":
		if !rightFixed || rightConstant <= 0 {
			return nil, fmt.Errorf("invalid task display divisor")
		}
		for key := range result {
			result[key] /= rightConstant
		}
	case "max":
		if !leftFixed || !rightFixed {
			return nil, fmt.Errorf("nonconstant task display max")
		}
		result[""] = max(leftConstant, rightConstant)
	}
	for _, amount := range result {
		if !displayAmountValid(amount) {
			return nil, fmt.Errorf("invalid task display amount")
		}
	}
	return result, nil
}

func taskDisplayConditions(node ast.Node, schema map[string]DisplayUsageField, negated bool, depth int, visits *int) ([][]DisplayUsageGuard, error) {
	*visits--
	if *visits < 0 {
		return nil, fmt.Errorf("task display work limit")
	}
	if depth > maxTaskDisplayDepth {
		return nil, fmt.Errorf("task display depth limit")
	}
	comparison, ok := node.(*ast.BinaryNode)
	if !ok {
		return nil, fmt.Errorf("unsupported task display condition")
	}
	if slices.Contains([]string{"&&", "and", "||", "or"}, comparison.Operator) {
		conjunction := comparison.Operator == "&&" || comparison.Operator == "and"
		left, err := taskDisplayConditions(comparison.Left, schema, negated, depth+1, visits)
		if err != nil {
			return nil, err
		}
		right, err := taskDisplayConditions(comparison.Right, schema, negated, depth+1, visits)
		if err != nil {
			return nil, err
		}
		if conjunction == negated {
			// A OR B is split into A and (NOT A AND B), so prices remain disjoint.
			notLeft, err := taskDisplayConditions(comparison.Left, schema, !negated, depth+1, visits)
			if err != nil {
				return nil, err
			}
			combined, err := taskDisplayConditionProduct(notLeft, right, schema, visits)
			if err != nil {
				return nil, err
			}
			if len(left)+len(combined) > maxTaskDisplayBranches {
				return nil, fmt.Errorf("task display branch limit")
			}
			return append(left, combined...), nil
		}
		return taskDisplayConditionProduct(left, right, schema, visits)
	}
	values := make(map[string]string)
	if comparison.Operator == ">" {
		key, known := displayUsageKey(comparison.Left)
		field, declared := schema[key]
		value, literal := requestRuleNumber(comparison.Right)
		if !known || key == "" || !declared || len(field.Enum) > 0 || !slices.Contains([]string{"second", "count", "credit"}, field.Unit) || !literal || !displayAmountValid(value) {
			return nil, fmt.Errorf("unsupported task display numeric condition")
		}
		return [][]DisplayUsageGuard{{{Values: values, Negated: negated, Numeric: &DisplayNumericGuard{Field: key, Unit: field.Unit, Operator: "gt", Value: value}}}}, nil
	}
	if !displayUsageValues(node, schema, values) {
		return nil, fmt.Errorf("unsupported task display condition")
	}
	return [][]DisplayUsageGuard{{{Values: values, Negated: negated}}}, nil
}

func taskDisplayConditionProduct(left, right [][]DisplayUsageGuard, schema map[string]DisplayUsageField, visits *int) ([][]DisplayUsageGuard, error) {
	var result [][]DisplayUsageGuard
	for _, a := range left {
		for _, b := range right {
			*visits--
			if *visits < 0 {
				return nil, fmt.Errorf("task display work limit")
			}
			guards := append(slices.Clone(a), b...)
			if taskDisplayGuardsPossible(guards, schema) {
				if len(result) >= maxTaskDisplayBranches {
					return nil, fmt.Errorf("task display branch limit")
				}
				result = append(result, guards)
			}
		}
	}
	return result, nil
}

func taskDisplayGuardsPossible(guards []DisplayUsageGuard, schema map[string]DisplayUsageField) bool {
	allowed := make(map[string][]string)
	lower := make(map[string]float64)
	upper := make(map[string]float64)
	for _, guard := range guards {
		if guard.Numeric != nil {
			key, value := guard.Numeric.Field, guard.Numeric.Value
			if guard.Negated {
				if previous, found := upper[key]; !found || value < previous {
					upper[key] = value
				}
			} else {
				if previous, found := lower[key]; !found || value > previous {
					lower[key] = value
				}
			}
			minimum, hasLower := lower[key]
			maximum, hasUpper := upper[key]
			if hasLower && hasUpper && minimum >= maximum {
				return false
			}
		}
		if guard.Negated && len(guard.Values) > 1 {
			continue
		}
		for key, value := range guard.Values {
			values, found := allowed[key]
			if !found {
				values = slices.Clone(schema[key].Enum)
			}
			values = slices.DeleteFunc(values, func(candidate string) bool { return (candidate == value) == guard.Negated })
			if len(values) == 0 {
				return false
			}
			allowed[key] = values
		}
	}
	return true
}

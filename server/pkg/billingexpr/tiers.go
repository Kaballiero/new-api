package billingexpr

import (
	"fmt"

	"github.com/expr-lang/expr/ast"
)

// Tier is one pricing leaf of an expression, discovered statically from the
// AST without running the program. Coefficients are USD per token for each
// referenced variable; Linear is false when the leaf body is not a plain sum
// of variable * constant terms and therefore has no per-unit price.
type Tier struct {
	Label        string
	Conditions   []TierCondition
	Coefficients map[string]float64
	Linear       bool
}

// TierCondition is one guard on the path from the expression root to a tier,
// lowered from a ternary condition. A comparison has Var (an environment
// variable such as "len", or a probe function such as "hour"), Arg (the literal
// probe argument, "UTC" in hour("UTC")), Op and Value. A boolean combination
// has Op "&&" or "||" with Operands. Negated marks the else branch.
type TierCondition struct {
	Var      string
	Arg      string
	Op       string
	Value    float64
	Negated  bool
	Operands []TierCondition
}

// EnumerateTiers returns every tier an expression can select, in source order,
// with the guards that select it and the linear coefficients of its body.
// It fails when the expression does not compile or its structure is not a
// conditional chain of tier() leaves (or one bare body). The result is cached
// alongside the compiled program.
func EnumerateTiers(exprStr string) ([]Tier, error) {
	entry, err := compileEntryFromCacheByHash(exprStr, ExprHashString(exprStr))
	if err != nil {
		return nil, err
	}
	return entry.tiers, entry.tiersErr
}

func enumerateTiers(root ast.Node, env map[string]any) ([]Tier, error) {
	return collectTiers(root, nil, env)
}

func collectTiers(node ast.Node, guards []TierCondition, env map[string]any) ([]Tier, error) {
	if conditional, ok := node.(*ast.ConditionalNode); ok {
		guard, err := lowerCondition(conditional.Cond, env)
		if err != nil {
			return nil, err
		}
		then, err := collectTiers(conditional.Exp1, appendGuard(guards, guard), env)
		if err != nil {
			return nil, err
		}
		guard.Negated = !guard.Negated
		otherwise, err := collectTiers(conditional.Exp2, appendGuard(guards, guard), env)
		if err != nil {
			return nil, err
		}
		return append(then, otherwise...), nil
	}

	tier := Tier{Conditions: guards}
	body := node
	if call, ok := node.(*ast.CallNode); ok && isTierCall(call) {
		label, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			return nil, fmt.Errorf("tier label must be a string literal")
		}
		tier.Label = label.Value
		body = call.Arguments[1]
	}
	if containsTierCall(body) {
		return nil, fmt.Errorf("tier() must be a leaf of the conditional chain")
	}
	tier.Coefficients, tier.Linear = foldLinearBody(body, env)
	return []Tier{tier}, nil
}

func appendGuard(guards []TierCondition, guard TierCondition) []TierCondition {
	return append(guards[:len(guards):len(guards)], guard)
}

func containsTierCall(node ast.Node) bool {
	return ast.Find(node, func(part ast.Node) bool {
		identifier, ok := part.(*ast.IdentifierNode)
		return ok && identifier.Value == "tier"
	}) != nil
}

func isTierCall(call *ast.CallNode) bool {
	callee, ok := call.Callee.(*ast.IdentifierNode)
	return ok && callee.Value == "tier" && len(call.Arguments) == 2
}

// foldLinearBody reads a body of the form term ('+' term)* where a term is
// variable * constant (either order) or a bare variable. Any other shape —
// subtraction, division, constants, function calls, a variable used twice,
// variable * variable — is not linear and yields no coefficients.
func foldLinearBody(node ast.Node, env map[string]any) (map[string]float64, bool) {
	coefficients := make(map[string]float64)
	if !foldLinearTerms(node, env, coefficients) {
		return nil, false
	}
	return coefficients, true
}

func foldLinearTerms(node ast.Node, env map[string]any, coefficients map[string]float64) bool {
	if sum, ok := node.(*ast.BinaryNode); ok && sum.Operator == "+" {
		return foldLinearTerms(sum.Left, env, coefficients) && foldLinearTerms(sum.Right, env, coefficients)
	}
	variable, coefficient, ok := linearTerm(node, env)
	if !ok {
		return false
	}
	if _, seen := coefficients[variable]; seen {
		return false
	}
	coefficients[variable] = coefficient
	return true
}

func linearTerm(node ast.Node, env map[string]any) (string, float64, bool) {
	if variable, ok := tokenVariable(node, env); ok {
		return variable, 1, true
	}
	product, ok := node.(*ast.BinaryNode)
	if !ok || product.Operator != "*" {
		return "", 0, false
	}
	if variable, ok := tokenVariable(product.Left, env); ok {
		coefficient, isNumber := requestRuleNumber(product.Right)
		return variable, coefficient, isNumber
	}
	if variable, ok := tokenVariable(product.Right, env); ok {
		coefficient, isNumber := requestRuleNumber(product.Left)
		return variable, coefficient, isNumber
	}
	return "", 0, false
}

func tokenVariable(node ast.Node, env map[string]any) (string, bool) {
	identifier, ok := node.(*ast.IdentifierNode)
	if !ok {
		return "", false
	}
	_, isNumeric := env[identifier.Value].(float64)
	return identifier.Value, isNumeric
}

func lowerCondition(node ast.Node, env map[string]any) (TierCondition, error) {
	switch cond := node.(type) {
	case *ast.UnaryNode:
		if cond.Operator != "!" && cond.Operator != "not" {
			break
		}
		inner, err := lowerCondition(cond.Node, env)
		if err != nil {
			return TierCondition{}, err
		}
		inner.Negated = !inner.Negated
		return inner, nil
	case *ast.BinaryNode:
		switch cond.Operator {
		case "&&", "and", "||", "or":
			left, err := lowerCondition(cond.Left, env)
			if err != nil {
				return TierCondition{}, err
			}
			right, err := lowerCondition(cond.Right, env)
			if err != nil {
				return TierCondition{}, err
			}
			op := "&&"
			if cond.Operator == "||" || cond.Operator == "or" {
				op = "||"
			}
			return TierCondition{Op: op, Operands: []TierCondition{left, right}}, nil
		case "<", "<=", ">", ">=", "==", "!=":
			return lowerComparison(cond, env)
		}
	}
	return TierCondition{}, fmt.Errorf("unsupported tier condition %q", node.String())
}

func lowerComparison(cond *ast.BinaryNode, env map[string]any) (TierCondition, error) {
	if subject, ok := conditionSubject(cond.Left, env); ok {
		if value, isNumber := requestRuleNumber(cond.Right); isNumber {
			subject.Op, subject.Value = cond.Operator, value
			return subject, nil
		}
	}
	if subject, ok := conditionSubject(cond.Right, env); ok {
		if value, isNumber := requestRuleNumber(cond.Left); isNumber {
			subject.Op, subject.Value = flipComparison(cond.Operator), value
			return subject, nil
		}
	}
	return TierCondition{}, fmt.Errorf("unsupported tier condition %q", cond.String())
}

func conditionSubject(node ast.Node, env map[string]any) (TierCondition, bool) {
	if variable, ok := tokenVariable(node, env); ok {
		return TierCondition{Var: variable}, true
	}
	call, ok := node.(*ast.CallNode)
	if !ok || len(call.Arguments) != 1 {
		return TierCondition{}, false
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || !usesRequestProbe(callee) {
		return TierCondition{}, false
	}
	arg, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		return TierCondition{}, false
	}
	return TierCondition{Var: callee.Value, Arg: arg.Value}, true
}

func flipComparison(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	default:
		return op
	}
}

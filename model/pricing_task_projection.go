package model

import (
	"fmt"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// configuredVeoUsageOffers projects the declared Google duration-rate shape only.
// Unsupported expressions remain unknown; this never evaluates task billing.
func configuredVeoUsageOffers(pricing Pricing, channel *Channel, upstream string, generation *jsplugin.RoutingGeneration) []CatalogOffer {
	unknown := []CatalogOffer{
		{ID: "configured:video:unknown", Operation: constant.TaskActionTextToVideo, Mode: "sync", MainComponent: "video_output"},
		{ID: "configured:image-video:unknown", Operation: constant.TaskActionImageToVideo, Mode: "sync", MainComponent: "video_output"},
	}
	var schema map[string]jsplugin.UsageFieldSchema
	for _, candidate := range generation.LookupEndpointCandidates("POST", "/v1/videos", upstream) {
		if candidate.Plugin != nil && candidate.Plugin.Meta.Key == "google" && slices.Contains(candidate.Plugin.Meta.ChannelTypes, channel.Type) {
			schema = candidate.Plugin.Meta.UsageSchema
			break
		}
	}
	_, body := billingexpr.ParseExprVersion(pricing.BillingExpr)
	tree, err := parser.Parse(body)
	if err != nil || schema == nil || pricing.BillingMode != billing_setting.BillingModeTieredExpr {
		return unknown
	}
	root := tree.Node
	if call, ok := root.(*ast.CallNode); ok {
		name, named := call.Callee.(*ast.IdentifierNode)
		if !named || name.Value != "tier" || len(call.Arguments) != 2 {
			return unknown
		}
		if _, literal := call.Arguments[0].(*ast.StringNode); !literal {
			return unknown
		}
		root = call.Arguments[1]
	}
	product, ok := root.(*ast.BinaryNode)
	if !ok || product.Operator != "*" {
		return unknown
	}
	usage, ok := product.Left.(*ast.CallNode)
	if !ok || len(usage.Arguments) != 1 {
		return unknown
	}
	callee, ok := usage.Callee.(*ast.IdentifierNode)
	field, literal := usage.Arguments[0].(*ast.StringNode)
	if !ok || callee.Value != "u" || !literal || schema[field.Value].Type != "number" || schema[field.Value].Unit != "second" {
		return unknown
	}
	type branch struct {
		node   ast.Node
		guards []jsplugin.CatalogUsageCondition
		depth  int
	}
	pending := []branch{{node: product.Right}}
	var result []CatalogOffer
	for len(pending) > 0 {
		if len(pending)+len(result) > 64 {
			return unknown
		}
		current := pending[0]
		pending = pending[1:]
		if current.depth > 16 {
			return unknown
		}
		if conditional, ok := current.node.(*ast.ConditionalNode); ok {
			comparison, ok := conditional.Cond.(*ast.BinaryNode)
			if !ok || comparison.Operator != "==" {
				return unknown
			}
			probe, ok := comparison.Left.(*ast.CallNode)
			value, literal := comparison.Right.(*ast.StringNode)
			if !ok || !literal || len(probe.Arguments) != 1 {
				return unknown
			}
			name, ok := probe.Callee.(*ast.IdentifierNode)
			key, literal := probe.Arguments[0].(*ast.StringNode)
			if !ok || name.Value != "u" || !literal || !slices.Contains(schema[key.Value].Enum, value.Value) {
				return unknown
			}
			guard := jsplugin.CatalogUsageCondition{Field: key.Value, Value: value.Value}
			then := append(slices.Clone(current.guards), guard)
			guard.Negated = true
			otherwise := append(slices.Clone(current.guards), guard)
			pending = append(pending, branch{conditional.Exp1, then, current.depth + 1}, branch{conditional.Exp2, otherwise, current.depth + 1})
			continue
		}
		for key, field := range schema {
			if len(field.Enum) == 0 {
				continue
			}
			possible := false
			for _, value := range field.Enum {
				matches := true
				for _, guard := range current.guards {
					if guard.Field == key && ((value == guard.Value) == guard.Negated) {
						matches = false
						break
					}
				}
				possible = possible || matches
			}
			if !possible {
				return unknown
			}
		}
		var amount float64
		switch number := current.node.(type) {
		case *ast.IntegerNode:
			amount = float64(number.Value)
		case *ast.FloatNode:
			amount = number.Value
		default:
			return unknown
		}
		if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
			return unknown
		}
		for _, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo} {
			result = append(result, CatalogOffer{ID: fmt.Sprintf("configured:%s:%d", action, len(result)), Operation: action, Mode: "sync", MainComponent: "video_output", PublicUsageConditions: current.guards, PublicConditional: len(current.guards) > 0, Prices: []CatalogPrice{{Component: "video_output", Unit: "second", AmountUSD: &amount}}})
		}
	}
	return result
}

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
)

type pricingHandlerSource struct {
	Package  string
	Contract HandlerInfo
}

// Preserve published schema identities while reading the relocated HTTP adapters and real request DTOs.
var pricingHandlerSources = []pricingHandlerSource{
	{Package: "get_pricing", Contract: HandlerInfo{Name: "GetPricing"}},
	{Package: "get_personal_pricing", Contract: HandlerInfo{Name: "GetEffectivePricing", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/consumerPricingResponse"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "get_group_pricing", Contract: HandlerInfo{Name: "GetEffectivePricingByGroup", QueryParams: []QueryParam{{Name: "group", Type: "string"}}, RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/consumerPricingResponse"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "get_pricing_overview", Contract: HandlerInfo{Name: "GetEffectivePricingOverview", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/effectivePricingOverviewData"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "get_public_models", Contract: HandlerInfo{Name: "GetPublicPricingModels", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/publicPricingModelsPage"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "get_model_pricing", Contract: HandlerInfo{Name: "GetEffectivePricingModel", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/consumerPricingModelDetail"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "get_pricing_config", Contract: HandlerInfo{Name: "GetModelPricingConfig", RespType: "ModelPricingSnapshot"}},
	{Package: "update_pricing_config", Contract: HandlerInfo{Name: "UpdateModelPricingConfig", BodySchema: map[string]any{"properties": map[string]any{"changes": map[string]any{"items": map[string]any{"$ref": "#/components/schemas/ModelPricingChange"}, "type": "array"}}, "type": "object"}, RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"properties": map[string]any{"updated_models": map[string]any{"items": map[string]any{"type": "string"}, "type": "array"}}, "type": "object"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "preview_pricing", Contract: HandlerInfo{Name: "GetEffectivePricingPreview", RespType: "effectivePricingPreviewResponse"}},
	{Package: "get_ratio_config", Contract: HandlerInfo{Name: "GetRatioConfig", RespType: "Pricing", ErrorCodes: []ErrorCode{{Status: 403, Code: "ratio_config_disabled"}}}},
	{Package: "reset_model_ratios", Contract: HandlerInfo{Name: "ResetModelRatio"}},
	{Package: "fetch_upstream_ratios", Contract: HandlerInfo{Name: "FetchUpstreamRatios", BodyType: "UpstreamRequest", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"properties": map[string]any{"differences": map[string]any{}, "prices": map[string]any{}, "test_results": map[string]any{"$ref": "#/components/schemas/TestResult"}}, "type": "object"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
	{Package: "preview_channel_reserves", Contract: HandlerInfo{Name: "PreviewChannelReserves", RespSchema: channelReservesResponse, ErrorCodes: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}}}},
	{Package: "list_syncable_channels", Contract: HandlerInfo{Name: "GetSyncableChannels", RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/SyncableChannel"}, "message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"}}, "type": "object"}}},
}

func parsePricingHandlers() error {
	for _, source := range pricingHandlerSources {
		savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		dir := filepath.Join("internal/pricing", source.Package)
		if err := parseModels(dir); err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		found := false
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
				continue
			}
			info := analyzeHandler(fn)
			*info = source.Contract
			if source.Package == "update_pricing_config" {
				info.BodySchema = channelInlineSchema(modelTypes["Request"])
			}
			handlers[info.Name] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing relocated pricing handler: %s", source.Contract.Name)
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

// channelReservesResponse is the inline response of preview_channel_reserves:
// its DTO names (Output, Item, Provider) are not unique schema names.
var channelReservesResponse = func() map[string]any {
	str := map[string]any{"type": "string"}
	nullableString := map[string]any{"type": "string", "nullable": true}
	nullableNumber := map[string]any{"type": "number", "nullable": true}
	provider := map[string]any{"type": "object", "required": []string{"tag", "tier", "usd"}, "properties": map[string]any{
		"tag": str, "tier": map[string]any{"type": "string", "enum": []string{"default", "priority", "flex", "ultrafast"}}, "usd": nullableNumber,
	}}
	item := map[string]any{"type": "object", "required": []string{"model", "upstream_model", "in_channel", "configured_source", "manual_usd", "configured_usd", "effective_source", "admission_policy", "example_usd", "example_status", "example_error", "providers"}, "properties": map[string]any{
		"model":             str,
		"upstream_model":    nullableString,
		"in_channel":        map[string]any{"type": "boolean"},
		"configured_source": map[string]any{"type": "string", "nullable": true, "enum": []any{"upstream", "configured_price", nil}},
		"manual_usd":        nullableNumber,
		"configured_usd":    nullableNumber,
		"effective_source":  map[string]any{"type": "string", "nullable": true, "enum": []any{"catalog", "catalog_stale", "live", "configured_price", "manual", nil}},
		"admission_policy":  map[string]any{"type": "string", "nullable": true, "enum": []any{"model_approximate_reserve", "model_reserve_add_on", "model_reserve_floor", "sonar_research_approximate_reserve", nil}},
		"example_usd":       nullableNumber,
		"example_status":    map[string]any{"type": "string", "enum": []string{"ok", "live_only", "needs_live_check", "refused"}},
		"example_error":     nullableString,
		"providers":         map[string]any{"type": "array", "items": provider},
	}}
	data := map[string]any{"type": "object", "required": []string{"fetched_at", "catalog_stale", "reference", "items"}, "properties": map[string]any{
		"fetched_at":    map[string]any{"type": "string", "format": "date-time", "nullable": true},
		"catalog_stale": map[string]any{"type": "boolean"},
		"reference": map[string]any{"type": "object", "required": []string{"operation", "prompt_tokens", "output_tokens"}, "properties": map[string]any{
			"operation": str, "prompt_tokens": map[string]any{"type": "integer"}, "output_tokens": map[string]any{"type": "integer"},
		}},
		"items": map[string]any{"type": "array", "items": item},
	}}
	return map[string]any{"type": "object", "properties": map[string]any{"data": data, "message": str, "success": map[string]any{"type": "boolean"}}}
}()

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
)

type channelHandlerSource struct {
	Name, Package, Body, ActualBody, InlineBody, Response, Projection string
	QueryOrder                                                        []string
	Errors                                                            []ErrorCode
}

var channelHandlerSources = []channelHandlerSource{
	{Name: "AddChannel", Package: "create_channel", Body: "AddChannelRequest", ActualBody: "AddChannelRequest", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 400, Code: "channel_validation_failed"}, {Status: 400, Code: "mode_unsupported"}, {Status: 500, Code: "internal_error"}}},
	{Name: "ApplyAllChannelUpstreamModelUpdates", Package: "apply_all_upstream_model_updates", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "apply_all", QueryOrder: []string{}},
	{Name: "ApplyChannelUpstreamModelUpdates", Package: "apply_upstream_model_updates", Body: "applyChannelUpstreamModelUpdatesRequest", ActualBody: "Input", InlineBody: "", Response: "", Projection: "output", QueryOrder: []string{}},
	{Name: "BatchSetChannelTag", Package: "set_channel_tags_batch", Body: "ChannelBatch", ActualBody: "Input", InlineBody: "", Response: "", Projection: "integer", QueryOrder: []string{}},
	{Name: "BatchUpdateChannelStatus", Package: "update_channel_status_batch", Body: "ChannelStatusBatchRequest", ActualBody: "Input", InlineBody: "", Response: "", Projection: "integer", QueryOrder: []string{}},
	{Name: "ChannelListModels", Package: "list_channel_models", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "ClearChannelAffinityCache", Package: "clear_affinity_cache", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "output", QueryOrder: []string{"all", "rule_name"}},
	{Name: "CopyChannel", Package: "copy_channel", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "copy", QueryOrder: []string{"suffix", "reset_balance"}},
	{Name: "DeleteChannel", Package: "delete_channel", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "DeleteChannelBatch", Package: "delete_channels_batch", Body: "ChannelBatch", ActualBody: "Input", InlineBody: "", Response: "", Projection: "integer", QueryOrder: []string{}},
	{Name: "DeleteDisabledChannel", Package: "delete_disabled_channels", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "integer", QueryOrder: []string{}},
	{Name: "DetectAllChannelUpstreamModelUpdates", Package: "detect_all_upstream_model_updates", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "task", QueryOrder: []string{}},
	{Name: "DetectChannelUpstreamModelUpdates", Package: "detect_upstream_model_updates", Body: "applyChannelUpstreamModelUpdatesRequest", ActualBody: "Input", InlineBody: "", Response: "", Projection: "detect", QueryOrder: []string{}},
	{Name: "DisableTagChannels", Package: "disable_tag_channels", Body: "ChannelTag", ActualBody: "Input", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "EditTagChannels", Package: "edit_tag_channels", Body: "ChannelTag", ActualBody: "ChannelTag", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "EnableTagChannels", Package: "enable_tag_channels", Body: "ChannelTag", ActualBody: "Input", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "EnabledListModels", Package: "list_enabled_channel_models", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "FetchModels", Package: "preview_channel_models", Body: "FetchModelsRequest", ActualBody: "FetchModelsRequest", InlineBody: "", Response: "", Projection: "string", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 502, Code: "fetch_models_failed"}}},
	{Name: "FetchUpstreamModels", Package: "fetch_upstream_models", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}, {Status: 500, Code: "internal_error"}, {Status: 502, Code: "fetch_models_failed"}}},
	{Name: "FixChannelsAbilities", Package: "repair_channel_abilities", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "repair", QueryOrder: []string{}},
	{Name: "GetAllChannels", Package: "list_channels", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "list", QueryOrder: []string{"p", "page_size", "id_sort", "sort_by", "sort_order", "tag_mode", "group", "status", "type"}},
	{Name: "GetChannel", Package: "get_channel", Body: "", ActualBody: "", InlineBody: "", Response: "Channel", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "GetChannelAffinityCacheStats", Package: "get_affinity_cache_stats", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "affinity", QueryOrder: []string{}},
	{Name: "GetChannelAffinityUsageCacheStats", Package: "get_affinity_usage_stats", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "usage_affinity", QueryOrder: []string{"rule_name", "using_group", "key_fp"}},
	{Name: "GetChannelKey", Package: "reveal_channel_key", Body: "", ActualBody: "", InlineBody: "", Response: "ChannelKeyResponse", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}}},
	{Name: "GetChannelOps", Package: "get_channel_operations", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "output", QueryOrder: []string{}},
	{Name: "GetCodexChannelRateLimitResetCredits", Package: "get_codex_reset_credits", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "GetCodexChannelUsage", Package: "get_codex_usage", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "GetTagModels", Package: "get_tag_models", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "string", QueryOrder: []string{"tag"}},
	{Name: "ManageMultiKeys", Package: "manage_multi_keys", Body: "MultiKeyManageRequest", ActualBody: "MultiKeyManageRequest", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "OllamaDeleteModel", Package: "delete_ollama_model", Body: "", ActualBody: "", InlineBody: "Input", Response: "", Projection: "ollama", QueryOrder: []string{}},
	{Name: "OllamaPullModel", Package: "pull_ollama_model", Body: "", ActualBody: "", InlineBody: "Input", Response: "", Projection: "ollama", QueryOrder: []string{}},
	{Name: "OllamaPullModelStream", Package: "stream_ollama_model_pull", Body: "", ActualBody: "", InlineBody: "Input", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "OllamaVersion", Package: "get_ollama_version", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "version", QueryOrder: []string{}},
	{Name: "RefreshCodexChannelCredential", Package: "refresh_codex_credential", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "refresh", QueryOrder: []string{}},
	{Name: "ResetCodexChannelUsage", Package: "reset_codex_usage", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "", QueryOrder: []string{}},
	{Name: "SearchChannels", Package: "search_channels", Body: "", ActualBody: "", InlineBody: "", Response: "SearchChannelsResponse", Projection: "", QueryOrder: []string{"keyword", "group", "model", "status", "id_sort", "sort_by", "sort_order", "tag_mode", "type", "p", "page_size"}, Errors: []ErrorCode{{Status: 500, Code: "internal_error"}}},
	{Name: "TestAllChannels", Package: "test_channels", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "task", QueryOrder: []string{}},
	{Name: "TestChannel", Package: "test_channel", Body: "", ActualBody: "", InlineBody: "", Response: "ChannelTestResponse", Projection: "", QueryOrder: []string{"model", "endpoint_type", "stream"}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}, {Status: 404, Code: "channel_not_found"}, {Status: 500, Code: "internal_error"}}},
	{Name: "UpdateAllChannelsBalance", Package: "update_channels_balances", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "UpdateChannel", Package: "update_channel", Body: "", ActualBody: "", InlineBody: "", Response: "PatchChannel", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 404, Code: "channel_not_found"}, {Status: 400, Code: "channel_validation_failed"}, {Status: 500, Code: "internal_error"}}},
	{Name: "UpdateChannelBalance", Package: "update_channel_balance", Body: "", ActualBody: "", InlineBody: "", Response: "", Projection: "empty", QueryOrder: []string{}},
	{Name: "UpdateChannelStatus", Package: "update_channel_status", Body: "ChannelStatusRequest", ActualBody: "Input", InlineBody: "", Response: "", Projection: "boolean", QueryOrder: []string{}},
	{Name: "ValidateModels", Package: "validate_models", Body: "ValidateModelsRequest", ActualBody: "ValidateModelsRequest", InlineBody: "", Response: "ValidateModelsResponse", Projection: "", QueryOrder: []string{}, Errors: []ErrorCode{{Status: 400, Code: "invalid_params"}}},
}

// Read relocated HTTP and DTO sources. Projection preserves the published SDK
// surface, including old unknown fields; this refactor does not revise contracts.
func parseChannelHandlers() error {
	for _, source := range channelHandlerSources {
		savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		dir := filepath.Join("internal/channels", source.Package)
		if err := parseModels(dir); err != nil {
			return err
		}
		keep := func(public, actual string) {
			if value := modelTypes[actual]; value != nil {
				savedTypes[public] = value
				delete(savedAliases, public)
			}
		}
		if source.Body != "" {
			keep(source.Body, source.ActualBody)
		}
		switch source.Name {
		case "GetChannelKey":
			keep("ChannelKeyResponse", "ChannelKeyResponse")
		case "SearchChannels":
			keep("SearchChannelsResponse", "SearchChannelsResponse")
		case "UpdateChannel":
			keep("PatchChannel", "PatchChannel")
		case "TestChannel":
			keep("ChannelTestResponse", "ChannelTestResponse")
		case "ValidateModels":
			keep("ValidateModelsRequest", "ValidateModelsRequest")
			keep("ModelValidationResult", "ModelValidationResult")
			keep("ValidateModelsSummary", "Summary")
			keep("ValidateModelsResponse", "ValidateModelsResponse")
			for _, field := range savedTypes["ValidateModelsResponse"].Fields.List {
				if id, ok := field.Type.(*ast.Ident); ok && id.Name == "Summary" {
					id.Name = "ValidateModelsSummary"
				}
			}
		case "ApplyAllChannelUpstreamModelUpdates":
			keep("applyAllChannelUpstreamModelUpdatesResult", "Result")
			for _, field := range modelTypes["Output"].Fields.List {
				if array, ok := field.Type.(*ast.ArrayType); ok {
					if id, ok := array.Elt.(*ast.Ident); ok && id.Name == "Result" {
						id.Name = "applyAllChannelUpstreamModelUpdatesResult"
					}
				}
			}
		case "DetectChannelUpstreamModelUpdates":
			keep("detectChannelUpstreamModelUpdatesResult", "Output")
		case "GetChannelAffinityCacheStats":
			keep("ChannelAffinityCacheStats", "ChannelAffinityCacheStats")
		case "GetChannelAffinityUsageCacheStats":
			keep("ChannelAffinityUsageCacheStats", "ChannelAffinityUsageCacheStats")
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
				continue
			}
			info := analyzeHandler(fn)
			info.Name = source.Name
			info.BodyType = source.Body
			info.RespType = source.Response
			info.RespSchema = nil
			info.RespIsList = false
			info.RespIsPaged = false
			info.RespStatus = 0
			if source.InlineBody != "" {
				info.BodySchema = channelInlineSchema(modelTypes[source.InlineBody])
			} else {
				info.BodySchema = nil
			}
			properties := map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}}
			envelope := map[string]any{"type": "object", "properties": properties}
			inline := func() map[string]any { return channelInlineSchema(modelTypes["Output"]) }
			unknown := func(schema map[string]any, fields ...string) {
				props, _ := schema["properties"].(map[string]any)
				for _, field := range fields {
					props[field] = map[string]any{}
				}
			}
			switch source.Projection {
			case "list":
				data := inline()
				unknown(data, "type_counts")
				properties["data"] = data
			case "repair":
				data := inline()
				unknown(data, "fails")
				properties["data"] = data
			case "output", "apply_all":
				properties["data"] = inline()
			case "copy":
				data := inline()
				unknown(data, "id")
				properties["data"] = data
			case "refresh":
				data := inline()
				unknown(data, "channel_id", "channel_type", "channel_name")
				properties["data"] = data
			case "detect":
				properties["data"] = map[string]any{"$ref": "#/components/schemas/detectChannelUpstreamModelUpdatesResult"}
			case "affinity":
				properties["data"] = map[string]any{"$ref": "#/components/schemas/ChannelAffinityCacheStats"}
			case "usage_affinity":
				properties["data"] = map[string]any{"$ref": "#/components/schemas/ChannelAffinityUsageCacheStats"}
			case "version":
				data := inline()
				unknown(data, "version")
				properties["data"] = data
			case "integer", "boolean", "string":
				properties["data"] = map[string]any{"type": source.Projection}
			case "task":
				properties["data"] = map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}, "status": map[string]any{"type": "string"}}}
			case "ollama":
				properties["message"] = map[string]any{}
			}
			if source.Projection != "" {
				info.RespSchema = envelope
			}
			if source.Name == "AddChannel" {
				info.RespStatus = 201
			}
			// Domain errors are written by the module transport, so the
			// published error codes of each handler are pinned in the table.
			info.ErrorCodes = source.Errors
			// Retain the original parameter order while obtaining names/types from HTTP.
			params := make([]QueryParam, 0, len(info.QueryParams))
			for _, name := range source.QueryOrder {
				for _, param := range info.QueryParams {
					if param.Name == name {
						params = append(params, param)
					}
				}
			}
			if len(params) != len(info.QueryParams) {
				return fmt.Errorf("channel %s query contract changed", source.Name)
			}
			if len(params) == 0 {
				params = nil
			}
			info.QueryParams = params
			handlers[source.Name] = info
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

// The old inline Gin maps did not declare required fields. Named DTO schemas
// continue to use the standard struct renderer.
func channelInlineSchema(value *ast.StructType) map[string]any {
	schema := structToSchema(value)
	delete(schema, "required")
	return schema
}

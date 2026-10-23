package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Preserve published operation shapes across the controller-to-scenario move.
// DTO schemas still come from the real record and response declarations.
var usageHandlerContracts = []HandlerInfo{
	{Name: "GetAdminUserLogs",
		ErrorCodes: []ErrorCode{{Status: 400,
			Code: "invalid_params"}}},

	{Name: "GetAllFlowQuotaDates",
		QueryParams: []QueryParam{{Name: "username",
			Type: "string"}},
		RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/FlowQuotaData"},
			"message": map[string]any{"type": "string"},
			"success": map[string]any{"type": "boolean"}},
			"type": "object"},
	},

	{Name: "GetAllLogs", ErrorCodes: []ErrorCode{{Status: 500, Code: "internal_error"}},
		QueryParams: []QueryParam{{Name: "p",
			Type: "integer"},
			{Name: "page_size",
				Type: "integer"},
			{Name: "type",
				Type: "integer"},
			{Name: "start_timestamp",
				Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "username",
				Type: "string"},
			{Name: "token_name",
				Type: "string"},
			{Name: "model_name",
				Type: "string"},
			{Name: "channel",
				Type: "string"},
			{Name: "group",
				Type: "string"},
			{Name: "request_id",
				Type: "string"},
			{Name: "upstream_request_id",
				Type: "string"}},
		RespIsPaged: true,
	},

	{Name: "GetAllQuotaDates",
		QueryParams: []QueryParam{{Name: "start_timestamp",
			Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "username",
				Type: "string"}},
		RespType: "QuotaData",
		ErrorCodes: []ErrorCode{{Status: 500,
			Code: "internal_error"}}},

	{Name: "GetLogAggregate"},

	{Name: "GetLogByKey"},

	{Name: "GetLogsSelfStat", ErrorCodes: []ErrorCode{{Status: 500, Code: "internal_error"}},
		QueryParams: []QueryParam{{Name: "type",
			Type: "integer"},
			{Name: "start_timestamp",
				Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "token_name",
				Type: "string"},
			{Name: "model_name",
				Type: "string"},
			{Name: "channel",
				Type: "string"},
			{Name: "group",
				Type: "string"}},
		RespType: "Stat",
	},

	{Name: "GetLogsStat", ErrorCodes: []ErrorCode{{Status: 500, Code: "internal_error"}},
		QueryParams: []QueryParam{{Name: "type",
			Type: "integer"},
			{Name: "start_timestamp",
				Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "token_name",
				Type: "string"},
			{Name: "username",
				Type: "string"},
			{Name: "model_name",
				Type: "string"},
			{Name: "channel",
				Type: "string"},
			{Name: "group",
				Type: "string"}},
		RespType: "Stat",
	},

	{Name: "GetQuotaDatesByUser",
		QueryParams: []QueryParam{{Name: "start_timestamp",
			Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"}},
		RespType: "QuotaData",
		ErrorCodes: []ErrorCode{{Status: 500,
			Code: "internal_error"}}},

	{Name: "GetRankings", RespSchema: map[string]any{
		"type": "object", "properties": map[string]any{
			"data":    map[string]any{"$ref": "#/components/schemas/RankingsResponse"},
			"message": map[string]any{"type": "string"}, "success": map[string]any{"type": "boolean"},
		},
	}},

	{Name: "GetUsageAggregate"},

	{Name: "GetUserFlowQuotaDates",
		RespSchema: map[string]any{"properties": map[string]any{"data": map[string]any{"$ref": "#/components/schemas/FlowQuotaData"},
			"message": map[string]any{"type": "string"},
			"success": map[string]any{"type": "boolean"}},
			"type": "object"},
	},

	{Name: "GetUserLogs",
		QueryParams: []QueryParam{{Name: "p",
			Type: "integer"},
			{Name: "page_size",
				Type: "integer"},
			{Name: "type",
				Type: "integer"},
			{Name: "start_timestamp",
				Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "token_name",
				Type: "string"},
			{Name: "model_name",
				Type: "string"},
			{Name: "group",
				Type: "string"},
			{Name: "request_id",
				Type: "string"},
			{Name: "upstream_request_id",
				Type: "string"}},
		RespIsPaged: true,
		ErrorCodes: []ErrorCode{{Status: 400,
			Code: "invalid_params"},
			{Status: 404,
				Code: "token_not_found"},
			{Status: 500,
				Code: "internal_error"}}},

	{Name: "GetUserQuotaDates",
		QueryParams: []QueryParam{{Name: "start_timestamp",
			Type: "integer"},
			{Name: "end_timestamp",
				Type: "integer"},
			{Name: "token_id",
				Type: "integer"}},
		RespType: "QuotaData",
		ErrorCodes: []ErrorCode{{Status: 400,
			Code: "invalid_token_id"},
			{Status: 400,
				Code: "time_span_exceeded"},
			{Status: 500,
				Code: "internal_error"}}},

	{Name: "QueryAdminLogs",
		ErrorCodes: []ErrorCode{{Status: 400,
			Code: "invalid_params"}}},

	{Name: "SearchAllLogs",
		RespSchema: map[string]any{"properties": map[string]any{"message": map[string]any{"type": "string"},
			"success": map[string]any{"type": "boolean"}},
			"type": "object"},
	},

	{Name: "SearchUserLogs",
		RespSchema: map[string]any{"properties": map[string]any{"message": map[string]any{"type": "string"},
			"success": map[string]any{"type": "boolean"}},
			"type": "object"},
	},
}

func parseUsageHandlers() error {
	sources := map[string][2]string{
		"GetAdminUserLogs":      {"admin_query_user_logs", "Get"},
		"QueryAdminLogs":        {"admin_query_user_logs", "Post"},
		"GetAllLogs":            {"admin_list_logs", "Handle"},
		"GetUserLogs":           {"list_own_logs", "Handle"},
		"GetLogByKey":           {"list_key_logs", "Handle"},
		"GetLogsStat":           {"admin_get_log_stats", "Handle"},
		"GetLogsSelfStat":       {"get_own_log_stats", "Handle"},
		"GetAllQuotaDates":      {"admin_list_usage_data", "Handle"},
		"GetQuotaDatesByUser":   {"admin_list_user_usage_data", "Handle"},
		"GetUserQuotaDates":     {"list_own_usage_data", "Handle"},
		"GetAllFlowQuotaDates":  {"admin_get_usage_flow", "Handle"},
		"GetUserFlowQuotaDates": {"get_own_usage_flow", "Handle"},
		"GetLogAggregate":       {"aggregate_logs", "Handle"},
		"GetUsageAggregate":     {"aggregate_usage_data", "Handle"},
		"GetRankings":           {"get_rankings", "Handle"},
		"SearchAllLogs":         {"transport", "Deprecated"},
		"SearchUserLogs":        {"transport", "Deprecated"},
	}
	for _, contract := range usageHandlerContracts {
		source := sources[contract.Name]
		path := filepath.Join("internal/usage", source[0], "http.go")
		if source[0] == "transport" {
			path = "internal/usage/transport/logs.go"
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		found := false
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if ok && fn.Name.Name == source[1] {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("missing relocated usage handler: %s", contract.Name)
		}
		handlers[contract.Name] = &contract
	}
	return nil
}

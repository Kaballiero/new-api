package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

var apiKeyHandlerSources = map[string]string{
	"GetAllTokens": "list_keys", "SearchTokens": "search_keys", "GetToken": "get_key", "GetTokenKey": "reveal_key", "DeleteToken": "delete_key", "GetTokenStatus": "get_key_status", "GetTokenUsage": "get_key_usage", "GetTokenAutoGroups": "get_key_groups", "AddToken": "create_key", "UpdateToken": "update_key", "DeleteTokenBatch": "delete_keys_batch", "GetTokenKeysBatch": "reveal_keys_batch",
}

func parseAPIKeyHandlers() error {
	savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
	if err := parseModels("internal/apikeys/dto"); err != nil {
		return err
	}
	// Public schema names keep their previous model embedding and custom decoder.
	request, response, autoGroups := modelTypes["Request"], modelTypes["Response"], modelTypes["AutoGroupsInput"]
	for _, field := range request.Fields.List {
		if len(field.Names) > 0 && field.Names[0].Name == "AutoGroups" {
			field.Type = ast.NewIdent("tokenAutoGroupsInput")
		}
	}
	modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	modelTypes["tokenRequest"], modelTypes["tokenResponse"], modelTypes["tokenAutoGroupsInput"] = request, response, autoGroups
	// Domain errors are rendered by the shared transport switch; only errors used
	// by each scenario are included.
	failures, err := transportFailureCodes("internal/apikeys/transport/errors.go")
	if err != nil {
		return fmt.Errorf("API key transport: %w", err)
	}
	names := make([]string, 0, len(apiKeyHandlerSources))
	for name := range apiKeyHandlerSources {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		dir := filepath.Join("internal/apikeys", apiKeyHandlerSources[name])
		savedTypes, savedAliases, savedFields = maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		if err := parseModels(dir); err != nil {
			return err
		}
		switch name {
		case "GetTokenKey":
			savedTypes["TokenKeyResponse"] = modelTypes["TokenKeyResponse"]
		case "DeleteTokenBatch", "GetTokenKeysBatch":
			savedTypes["TokenBatch"] = modelTypes["Request"]
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Handle" {
				continue
			}
			info := analyzeHandler(fn)
			info.Name = name
			switch name {
			case "AddToken", "UpdateToken":
				info.BodyType = "tokenRequest"
				info.RespType = "tokenResponse"
				if name == "AddToken" {
					info.RespStatus = 201
				}
			case "GetToken":
				info.RespType = "tokenResponse"
			case "GetTokenKey":
				info.RespType = "TokenKeyResponse"
			case "DeleteTokenBatch", "GetTokenKeysBatch":
				info.BodyType = "TokenBatch"
			}
			preserveAPIKeyResponse(info)
			business, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "usecase.go"), nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			seen := map[ErrorCode]bool{}
			for _, code := range info.ErrorCodes {
				seen[code] = true
			}
			// Scenario errors are referenced as domain.ErrX (or domain.X for
			// constructors such as domain.Internal) in usecase order.
			ast.Inspect(business, func(n ast.Node) bool {
				selector, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := selector.X.(*ast.Ident); !ok || pkg.Name != "domain" {
					return true
				}
				if code, ok := failures[strings.TrimPrefix(selector.Sel.Name, "Err")]; ok && !seen[code] {
					seen[code] = true
					info.ErrorCodes = append(info.ErrorCodes, code)
				}
				return true
			})
			handlers[name] = info
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	// GET /api/getapi/tokens has an explicit contract; only its item type is parsed.
	savedTypes, savedAliases, savedFields = maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
	if err := parseModels("internal/apikeys/list_getapi_keys"); err != nil {
		return err
	}
	key := modelTypes["Key"]
	modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	modelTypes["GetAPITokenKey"] = key
	return nil
}

// Preserve published projections where callback/method-result inference was
// incomplete before the move. This refactor does not revise those contracts.
func preserveAPIKeyResponse(info *HandlerInfo) {
	var data map[string]any
	switch info.Name {
	case "GetTokenAutoGroups":
		data = map[string]any{"type": "object", "properties": map[string]any{"groups": map[string]any{"type": "string"}, "max_count": map[string]any{"type": "integer"}}}
	case "DeleteTokenBatch":
		data = map[string]any{"type": "integer"}
	case "GetTokenKeysBatch":
		data = map[string]any{"type": "object", "properties": map[string]any{"keys": map[string]any{}}}
	case "GetTokenUsage":
		props := map[string]any{}
		for _, field := range []string{"object", "name"} {
			props[field] = map[string]any{"type": "string"}
		}
		for _, field := range []string{"total_granted", "total_used", "total_available", "expires_at"} {
			props[field] = map[string]any{"type": "integer"}
		}
		for _, field := range []string{"unlimited_quota", "model_limits_enabled"} {
			props[field] = map[string]any{"type": "boolean"}
		}
		props["model_limits"] = map[string]any{"$ref": "#/components/schemas/Model"}
		referencedTypes["Model"] = true
		data = map[string]any{"type": "object", "properties": props}
	}
	if data == nil {
		return
	}
	properties := map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": data}
	if info.Name == "GetTokenUsage" {
		properties["code"] = map[string]any{"type": "boolean"}
	}
	info.RespType = ""
	info.RespSchema = map[string]any{"type": "object", "properties": properties}
}

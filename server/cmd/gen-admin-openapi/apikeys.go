package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
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
	failures, err := apiKeyFailureCodes()
	if err != nil {
		return err
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
			ast.Inspect(business, func(n ast.Node) bool {
				literal, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, entry := range literal.Elts {
					kv, ok := entry.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Kind" {
						continue
					}
					value, ok := kv.Value.(*ast.BasicLit)
					if !ok || value.Kind != token.STRING {
						continue
					}
					kind, _ := strconv.Unquote(value.Value)
					if code, ok := failures[kind]; ok && !seen[code] {
						seen[code] = true
						info.ErrorCodes = append(info.ErrorCodes, code)
					}
				}
				return true
			})
			handlers[name] = info
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

// Business failure kinds are rendered by a shared transport helper. Discover
// their status/code mapping there, then include only kinds used by each scenario.
func apiKeyFailureCodes() (map[string]ErrorCode, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "internal/adapter/apikeyhttp/errors.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	result := map[string]ErrorCode{}
	ast.Inspect(file, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		var code *ErrorCode
		for _, stmt := range clause.Body {
			ast.Inspect(stmt, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if value := analyzeErrorCall(call); value != nil {
						code = value
					}
				}
				return true
			})
		}
		if code != nil {
			for _, expr := range clause.List {
				if literal, ok := expr.(*ast.BasicLit); ok {
					kind, _ := strconv.Unquote(literal.Value)
					result[kind] = *code
				}
			}
		}
		return true
	})
	if len(result) == 0 {
		return nil, fmt.Errorf("API key transport has no error code mappings")
	}
	return result, nil
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

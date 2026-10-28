package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
)

// Resolve stable controller names to the HTTP scenario and its actual DTOs.
func parsePersonalAccessTokenHandlers() error {
	for _, source := range []struct{ handler, scenario string }{
		{"GetAccessTokenStatus", "get_status"},
		{"GenerateAccessToken", "generate_token"},
		{"RevokeAccessToken", "revoke_token"},
	} {
		savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		dir := filepath.Join("internal/personal_access_tokens", source.scenario)
		if err := parseModels(dir); err != nil {
			return err
		}
		if source.handler == "GetAccessTokenStatus" {
			savedTypes["UserAccessTokenStatus"] = modelTypes["Output"]
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
			info.Name = source.handler
			// Interface-return inference is opaque; retain the existing public names.
			switch source.handler {
			case "GetAccessTokenStatus":
				info.RespType = "UserAccessTokenStatus"
				referencedTypes[info.RespType] = true
			case "GenerateAccessToken":
				info.RespType = ""
				info.RespSchema = structToSchema(modelTypes["Response"])
			case "RevokeAccessToken":
				// A null data response keeps the established generic ApiResponse schema.
				info.RespType = ""
				info.RespSchema = nil
			}
			handlers[source.handler] = info
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

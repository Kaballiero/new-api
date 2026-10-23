package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Resolve the unchanged router names to their scenario HTTP source; infer query/body facts from AST.
func parseRedemptionHandlers() error {
	for _, source := range []struct{ handler, scenario string }{
		{"GetAllRedemptions", "list_codes"}, {"SearchRedemptions", "search_codes"}, {"GetRedemption", "get_code"},
		{"AddRedemption", "create_codes"}, {"UpdateRedemption", "update_code"}, {"DeleteRedemption", "delete_code"},
		{"DeleteRedemptionBatch", "delete_codes"}, {"DeleteInvalidRedemption", "delete_invalid_codes"}, {"TopUp", "redeem_code"},
	} {
		dir := filepath.Join("internal/redemption", source.scenario)
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
			info.Name = source.handler
			// Execute and DTO projections are opaque to the parser. Keep the already published success envelope.
			var data map[string]any
			switch source.handler {
			case "AddRedemption":
				data = map[string]any{"type": "string"}
			case "UpdateRedemption":
				data = map[string]any{"$ref": "#/components/schemas/Redemption"}
			case "DeleteInvalidRedemption", "TopUp":
				data = map[string]any{"type": "integer"}
			}
			if data != nil {
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{
					"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": data}}
			}

			handlers[source.handler] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing redemption handler %s", source.scenario)
		}
	}
	return nil
}

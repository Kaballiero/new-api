package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// Follow the moved transports and infer the public projection from its technical reader.
func parseSettingsHandlers() error {
	if err := parseModels("internal/settings/update_option"); err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), "internal/settings/update_option/http.go", nil, parser.SkipObjectResolution)
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
		info.Name = "UpdateOption"
		handlers[info.Name] = info
		found = true
	}
	if !found {
		return fmt.Errorf("missing settings update handler")
	}
	file, err = parser.ParseFile(token.NewFileSet(), "internal/adapter/settings/settings/public.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != "ReadStatus" {
			continue
		}
		locals := collectLocals(fn.Body)
		data, ok := locals["data"].Init.(*ast.CompositeLit)
		if !ok {
			return fmt.Errorf("missing settings status projection")
		}
		schema := ginHSchema(data, locals)
		properties := schema["properties"].(map[string]any)
		// This late gateway callback retains the previously inferred concrete return shape.
		properties["billing_fx"] = refOrPrimitive("BillingFXStatus")
		handlers["GetStatus"] = &HandlerInfo{Name: "GetStatus", RespStatus: 200, RespSchema: map[string]any{
			"type": "object", "properties": map[string]any{
				"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": schema,
			},
		}}
		return nil
	}
	return fmt.Errorf("missing settings status reader")
}

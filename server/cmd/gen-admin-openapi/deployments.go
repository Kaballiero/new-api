package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// These responses keep the published projection while their handlers move out of controller.
func parseDeploymentHandlers() error {
	for name, scenario := range map[string]string{
		"GetModelDeploymentSettings": "get_deployment_settings",
		"TestIoNetConnection":        "test_deployment_connection",
		"GetDeploymentLogs":          "get_deployment_logs",
	} {
		path := filepath.Join("internal/deployments", scenario, "http.go")
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		found := false
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Handle" {
				continue
			}
			found = true
			fn.Name.Name = name
			info := analyzeHandler(fn)
			if name == "GetDeploymentLogs" {
				// The existing manifest publishes Log here even though runtime returns a raw JSON string.
				info.RespType = "Log"
				info.RespSchema = nil
			} else {
				properties := map[string]any{}
				if name == "GetModelDeploymentSettings" {
					properties = map[string]any{"provider": map[string]any{"type": "string"}, "enabled": map[string]any{"type": "boolean"}, "configured": map[string]any{"type": "boolean"}, "can_connect": map[string]any{"type": "boolean"}}
				} else {
					properties = map[string]any{"hardware_count": map[string]any{"type": "integer"}, "total_available": map[string]any{"type": "integer"}}
				}
				info.RespType = ""
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": map[string]any{"type": "object", "properties": properties}}}
			}
			handlers[name] = info
		}
		if !found {
			return fmt.Errorf("missing deployments handler: %s", path)
		}
	}
	return nil
}

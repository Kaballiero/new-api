package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Resolve existing router entrypoints to taskplugins transports. The usecase
// result is opaque to AST inference, so retain the already published wrappers.
func parseTaskPluginHandlers() error {
	if err := parseModels("internal/adapter/gorm/taskplugins/record"); err != nil {
		return err
	}
	file, err := parser.ParseFile(token.NewFileSet(), "internal/taskplugins/domain/plugin.go", nil, parser.SkipObjectResolution)
	if err != nil {
		return err
	}
	names := map[string]string{"Detail": "taskPluginDetail", "RuntimeStatus": "taskPluginRuntimeStatus", "RebuildOutcome": "taskPluginRebuildOutcome"}
	for _, decl := range file.Decls {
		group, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range group.Specs {
			typ, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			published := names[typ.Name.Name]
			if published == "" {
				continue
			}
			shape, ok := typ.Type.(*ast.StructType)
			if !ok {
				return fmt.Errorf("missing task plugin struct %s", typ.Name.Name)
			}
			for _, field := range shape.Fields.List {
				if len(field.Names) == 0 {
					continue
				}
				switch field.Names[0].Name {
				case "Plugin":
					field.Type = &ast.StarExpr{X: &ast.Ident{Name: "TaskPlugin"}}
				case "LastRebuild":
					field.Type = &ast.Ident{Name: "taskPluginRebuildOutcome"}
				}
			}
			modelTypes[published] = shape
			recordStructFields(published, shape)
		}
	}
	for _, source := range []struct{ handler, scenario, response string }{
		{"UploadTaskPlugin", "upload_task_plugin", "taskPluginDetail"},
		{"ListTaskPlugins", "list_task_plugins", ""},
		{"GetTaskPlugin", "get_task_plugin", "taskPluginDetail"},
		{"GetTaskPluginVersions", "list_task_plugin_versions", "TaskPlugin"},
		{"GetTaskPluginIcon", "fetch_task_plugin_icon", ""},
		{"GetTaskPluginRuntime", "get_task_plugin_runtime", "taskPluginRuntimeStatus"},
		{"ActivateTaskPlugin", "activate_task_plugin", ""},
		{"SetTaskPluginStatus", "set_task_plugin_status", ""},
		{"DeleteTaskPluginVersion", "delete_task_plugin_version", ""},
		{"DryRunTaskPlugin", "dryrun_task_plugin", ""},
		{"GetTaskPluginMarketplaceSources", "get_marketplace_sources", "TaskPluginMarketplaceSource"},
		{"UpdateTaskPluginMarketplaceSources", "update_marketplace_sources", "TaskPluginMarketplaceSource"},
		{"GetTaskPluginOptions", "list_bindable_task_plugins", ""},
	} {
		dir := filepath.Join("internal/taskplugins", source.scenario)
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
			if !ok || fn.Recv == nil || fn.Name.Name != "ServeHTTP" {
				continue
			}
			info := analyzeHandler(fn)
			info.Name = source.handler
			info.RespType = source.response
			if source.handler == "UpdateTaskPluginMarketplaceSources" {
				info.BodyType = "TaskPluginMarketplaceSource"
			}
			if source.handler == "SetTaskPluginStatus" {
				info.QueryParams = []QueryParam{{Name: "cascade", Type: "string"}, {Name: "force", Type: "string"}}
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{
					"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"},
					"data": map[string]any{"type": "object", "properties": map[string]any{
						"disabled_channels": map[string]any{"type": "integer"}, "plugin_enabled": map[string]any{},
					}},
				}}
			}
			handlers[source.handler] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing task plugin handler %s", source.handler)
		}
	}
	return nil
}

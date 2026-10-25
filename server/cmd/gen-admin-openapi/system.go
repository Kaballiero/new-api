package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// Resolve the moved system transports to the existing operation names and wire DTOs.
func parseSystemHandlers() error {
	uptime := modelTypes["UptimeGroupResult"]
	for _, dir := range []string{"internal/system/domain", "internal/system/dto", "internal/system/get_setup", "internal/system/complete_setup", "internal/system/get_performance_stats", "internal/system/list_system_instances", "internal/system/list_log_files"} {
		if err := parseModels(dir); err != nil {
			return err
		}
	}
	modelTypes["UptimeGroupResult"] = uptime
	recordStructFields("UptimeGroupResult", uptime)
	for _, entry := range []struct{ name, scenario string }{
		{"GetSetup", "get_setup"}, {"PostSetup", "complete_setup"}, {"TestStatus", "test_database_health"},
		{"ListSystemInstances", "list_system_instances"}, {"DeleteStaleSystemInstances", "sweep_stale_instances"}, {"DeleteStaleSystemInstance", "delete_system_instance"},
		{"CreateLogCleanupSystemTask", "enqueue_log_cleanup"}, {"GetCurrentSystemTask", "get_current_system_task"}, {"ListSystemTasks", "list_system_tasks"}, {"GetSystemTask", "get_system_task"},
		{"GetPerformanceStats", "get_performance_stats"}, {"ClearDiskCache", "clear_disk_cache"}, {"ResetPerformanceStats", "reset_performance_stats"}, {"ForceGC", "force_gc"}, {"GetLogFiles", "list_log_files"}, {"CleanupLogFiles", "delete_log_files"},
		{"GetPerfMetrics", "get_perf_metrics"}, {"GetPerfMetricsSummary", "get_perf_metrics_summary"}, {"GetUptimeKumaStatus", "get_uptime_status"},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("internal/system", entry.scenario, "http.go"), nil, parser.SkipObjectResolution)
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
			info.Name = entry.name
			switch entry.name {
			case "GetPerformanceStats":
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": refOrPrimitive("PerformanceStats")}}
			case "CleanupLogFiles":
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": map[string]any{"type": "object", "properties": map[string]any{"deleted_count": map[string]any{"type": "integer"}, "failed_files": map[string]any{"type": "string"}, "freed_bytes": map[string]any{"type": "integer"}}}}}
			}
			// These response messages remain strings when transport localization is opaque to AST inference.
			switch entry.name {
			case "TestStatus", "ForceGC", "ClearDiskCache", "ResetPerformanceStats":
				if properties, ok := info.RespSchema["properties"].(map[string]any); ok {
					properties["message"] = map[string]any{"type": "string"}
				}
			}

			handlers[entry.name] = info
			found = true
		}
		if !found {
			return fmt.Errorf("missing system handler %s", entry.name)
		}
	}
	return nil
}

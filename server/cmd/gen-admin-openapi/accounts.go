package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
)

// Account handlers retain their public operation names after moving to receiver
// methods. This is the same explicit contract mapping as the route manifest.
var accountHandlerSources = map[string]struct{ scenario, method string }{
	"GetAllUsers":           {"list_accounts", "Handle"},
	"SearchUsers":           {"search_accounts", "Handle"},
	"GetUsersBatch":         {"get_accounts_batch", "Handle"},
	"GetUser":               {"get_account", "Handle"},
	"AdminClearUserBinding": {"clear_binding", "Handle"},
	"CreateUser":            {"create_account", "Handle"},
	"UpdateUser":            {"update_account", "Handle"},
	"DeleteUser":            {"delete_account", "Handle"},
	"ManageUser":            {"manage_account", "Handle"},
	"BulkUpdateUserGroup":   {"change_group", "Handle"},
	"GetSelf":               {"get_profile", "Handle"},
	"UpdateSelf":            {"update_profile", "Handle"},
	"GetUserModels":         {"get_account_models", "Handle"},
	"GetAffCode":            {"get_affiliate_code", "Handle"},
	"TransferAffQuota":      {"transfer_affiliate_quota", "Handle"},
	"UpdateUserSetting":     {"update_notification_preferences", "Handle"},
	"GetCheckinStatus":      {"get_checkin_status", "Handle"},
	"DoCheckin":             {"check_in", "Handle"},
	"GetGroups":             {"get_groups", "List"},
	"GetUserGroups":         {"get_groups", "ForUser"},
	"ProvisionGetAPIUser":   {"provision_account", "Handle"},
}

func parseAccountHandlers() error {
	names := make([]string, 0, len(accountHandlerSources))
	for name := range accountHandlerSources {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		source := accountHandlerSources[name]
		dir := filepath.Join("internal/accounts", source.scenario)
		canonicalBatchRow := modelTypes["UserBatchRow"]
		// DTOs belong to their consuming scenario; model aliases keep the previous
		// names available to the manifest and generated client contracts.
		if err := parseModels(dir); err != nil {
			return fmt.Errorf("account DTO %s: %w", source.scenario, err)
		}
		// The public model name retains its original nullable timestamp schema.
		if name == "GetUsersBatch" {
			modelTypes["UserBatchRow"] = canonicalBatchRow
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "http.go"), nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != source.method || !looksLikeHandler(fn) {
				continue
			}
			info := analyzeHandler(fn)
			info.Name = name
			if name == "ProvisionGetAPIUser" {
				info.BodyType = "GetAPICreateUserRequest"
				info.RespType = "GetAPICreateCredential"
				info.RespStatus = 201
				referencedTypes["GetAPICreateCredential"] = true
			}
			// Preserve the existing inline check-in contracts when the transport
			// now writes named DTOs. Stats intentionally remains unspecified.
			if name == "DoCheckin" || name == "GetCheckinStatus" {
				fields := map[string]any{"quota_awarded": map[string]any{"type": "integer"}, "checkin_date": map[string]any{"type": "string"}}
				if name == "GetCheckinStatus" {
					fields = map[string]any{"enabled": map[string]any{"type": "boolean"}, "min_quota": map[string]any{"type": "integer"}, "max_quota": map[string]any{"type": "integer"}, "stats": map[string]any{}}
				}
				info.RespType = ""
				info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": map[string]any{"type": "object", "properties": fields}}}
			}
			handlers[name] = info
		}
	}
	return nil
}

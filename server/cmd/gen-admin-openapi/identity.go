package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
)

// Stable operation names resolve to the scenario that owns the HTTP contract.
var identityHandlerSources = map[string]struct{ scenario, method string }{
	"RefreshAuth":                 {"refresh_session", "Handle"},
	"AuthLogout":                  {"logout", "Handle"},
	"GetLoginSessions":            {"list_sessions", "Handle"},
	"DeleteLoginSession":          {"revoke_session", "Handle"},
	"RevokeOtherLoginSessions":    {"revoke_other_sessions", "Handle"},
	"PasskeyRegisterBegin":        {"begin_passkey_registration", "Handle"},
	"PasskeyRegisterFinish":       {"finish_passkey_registration", "Handle"},
	"PasskeyDelete":               {"delete_passkey", "Handle"},
	"PasskeyStatus":               {"get_passkey_status", "Handle"},
	"PasskeyLoginBegin":           {"begin_passkey_login", "Handle"},
	"PasskeyLoginFinish":          {"finish_passkey_login", "Handle"},
	"AdminResetPasskey":           {"admin_reset_passkey", "Handle"},
	"PasskeyVerifyBegin":          {"begin_passkey_verification", "Handle"},
	"PasskeyVerifyFinish":         {"finish_passkey_verification", "Handle"},
	"WeChatAuth":                  {"login_wechat", "Handle"},
	"WeChatBind":                  {"bind_wechat", "Handle"},
	"GetUserOAuthBindings":        {"get_oauth_bindings", "Self"},
	"GetUserOAuthBindingsByAdmin": {"get_oauth_bindings", "Admin"},
	"UnbindCustomOAuth":           {"unlink_oauth_account", "Handle"},
	"UnbindCustomOAuthByAdmin":    {"admin_unlink_oauth_account", "Handle"},
	"SendEmailVerification":       {"send_verification_email", "Handle"},
	"SendPasswordResetEmail":      {"request_password_reset", "Handle"},
	"ResetPassword":               {"recover_password", "Handle"},
	"GetPasswordEncryptionKey":    {"get_password_encryption_key", "Handle"},
	"Login":                       {"password_login", "Handle"},
	"Register":                    {"register_password", "Handle"},
	"DeleteSelf":                  {"delete_account", "Handle"},
	"Setup2FA":                    {"start_twofa_setup", "Handle"},
	"Enable2FA":                   {"finish_twofa_setup", "Handle"},
	"Disable2FA":                  {"disable_twofa", "Handle"},
	"Get2FAStatus":                {"get_twofa_status", "Handle"},
	"RegenerateBackupCodes":       {"regenerate_backup_codes", "Handle"},
	"Admin2FAStats":               {"get_twofa_stats", "Handle"},
	"AdminDisable2FA":             {"admin_disable_twofa", "Handle"},
	"EmailBindStart":              {"start_email_binding", "Handle"},
	"EmailBindResend":             {"resend_email_binding", "Handle"},
	"EmailBind":                   {"confirm_email_binding", "Handle"},
	"GetVerificationMethods":      {"get_verification_requirements", "Handle"},
	"UniversalVerify":             {"verify_security_input", "Handle"},
	"VerifyLogin":                 {"verify_login_code", "Handle"},
	"LoginPasskeyBegin":           {"begin_login_passkey", "Handle"},
	"LoginPasskeyFinish":          {"finish_login_passkey", "Handle"},
	"GenerateOAuthCode":           {"start_oauth_flow", "Handle"},
	"HandleOAuth":                 {"complete_oauth", "Handle"},
}

func parseIdentityHandlers() error {
	names := make([]string, 0, len(identityHandlerSources))
	for name := range identityHandlerSources {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		source := identityHandlerSources[name]
		dir := filepath.Join("internal/identity", source.scenario)
		savedTypes, savedAliases, savedFields := maps.Clone(modelTypes), maps.Clone(modelAliases), maps.Clone(fieldTypes)
		if err := parseModels(dir); err != nil {
			return fmt.Errorf("identity DTO %s: %w", source.scenario, err)
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
			// Local Request/Output names must not overwrite unrelated public schemas.
			if info.BodyType != "" {
				local := info.BodyType
				switch name {
				case "GenerateOAuthCode":
					info.BodyType = "oauthStateRequest"
				case "WeChatBind":
					info.BodyType = "wechatBindRequest"
				case "PasskeyVerifyBegin":
					info.BodyType = "passkeyVerifyBeginRequest"
					modelTypes[local] = savedTypes["PasskeyVerifyBeginRequest"]
				case "EmailBindStart", "EmailBindResend", "EmailBind":
					info.BodyType = "emailBindRequest"
					modelTypes[local] = savedTypes["EmailBindRequest"]
				case "VerifyLogin", "LoginPasskeyBegin", "LoginPasskeyFinish":
					info.BodySchema = structToSchema(modelTypes[local])
					info.BodyType = ""
				case "PasskeyRegisterFinish", "PasskeyLoginFinish", "PasskeyVerifyFinish":
					info.BodyType = ""
				}
				if info.BodyType != "" && info.BodyType != "User" {
					savedTypes[info.BodyType] = modelTypes[local]
					delete(savedAliases, info.BodyType)
				}
			}
			for _, typeName := range []string{"VerificationInput", "VerificationRequirements", "UserOAuthBindingResponse", "Verify2FARequest"} {
				if st := modelTypes[typeName]; st != nil {
					savedTypes[typeName] = st
					delete(savedAliases, typeName)
				}
			}
			preserveIdentityHandlerContract(info)
			handlers[name] = info
		}
		modelTypes, modelAliases, fieldTypes = savedTypes, savedAliases, savedFields
	}
	return nil
}

// Callback and interface results are opaque to the AST walker. Preserve the
// published projections rather than changing client contracts during the move.
func preserveIdentityHandlerContract(info *HandlerInfo) {
	switch info.Name {
	case "Setup2FA":
		info.RespType = "TwoFASetup"
	case "UniversalVerify":
		info.RespType = "SecurityProof"
	case "PasskeyVerifyFinish":
		info.RespType = "SecurityProof"
		// The published schema is the envelope of the former inline
		// "passkey disabled" response, which the walker used to see.
		info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}}}
	case "GetVerificationMethods":
		info.RespType = "VerificationRequirements"
	case "GetUserOAuthBindings", "GetUserOAuthBindingsByAdmin":
		info.RespType = ""
		info.RespSchema = map[string]any{"type": "object", "properties": map[string]any{"success": map[string]any{"type": "boolean"}, "message": map[string]any{"type": "string"}, "data": map[string]any{"$ref": "#/components/schemas/UserOAuthBindingResponse"}}}
		referencedTypes["UserOAuthBindingResponse"] = true
	}
	if info.RespType != "" {
		referencedTypes[info.RespType] = true
	}
	var data map[string]interface{}
	if info.RespSchema != nil {
		properties, _ := info.RespSchema["properties"].(map[string]interface{})
		data, _ = properties["data"].(map[string]interface{})
		if info.Name == "HandleOAuth" {
			properties["message"] = map[string]interface{}{}
			delete(properties, "data")
		}
	}
	switch info.Name {
	case "ResetPassword":
		if info.RespSchema != nil {
			properties, _ := info.RespSchema["properties"].(map[string]any)
			properties["data"] = map[string]any{"type": "string"}
		}
	case "EmailBind", "WeChatBind", "UnbindCustomOAuth":
		if data != nil {
			data["properties"] = map[string]interface{}{"notification_warning": map[string]interface{}{"type": "boolean"}}
		}
	case "PasskeyStatus":
		if data != nil {
			data["properties"] = map[string]interface{}{"enabled": map[string]interface{}{"type": "boolean"}, "last_used_at": map[string]interface{}{"type": "string", "format": "date-time"}}
		}
	case "PasskeyRegisterBegin", "PasskeyLoginBegin", "PasskeyVerifyBegin", "LoginPasskeyBegin":
		if data != nil {
			options := map[string]interface{}{}
			if info.Name != "PasskeyRegisterBegin" {
				options["$ref"] = "#/components/schemas/Log"
				referencedTypes["Log"] = true
			}
			data["properties"] = map[string]interface{}{"flow_token": map[string]interface{}{}, "expires_at": map[string]interface{}{}, "options": options}
		}
	}
}

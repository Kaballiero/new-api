package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// SelfUserData формирует безопасный ответ профиля для панели, входа и сессии.
func SelfUserData(profile domain.SelfProfile, sidebarModules string, permissions map[string]any) map[string]any {
	return map[string]any{
		"id":                profile.Id,
		"username":          profile.Username,
		"display_name":      profile.DisplayName,
		"has_password":      profile.HasPassword,
		"role":              profile.Role,
		"status":            profile.Status,
		"email":             profile.Email,
		"github_id":         profile.GitHubId,
		"discord_id":        profile.DiscordId,
		"oidc_id":           profile.OidcId,
		"wechat_id":         profile.WeChatId,
		"telegram_id":       profile.TelegramId,
		"group":             profile.Group,
		"quota":             profile.Quota,
		"used_quota":        profile.UsedQuota,
		"request_count":     profile.RequestCount,
		"aff_code":          profile.AffCode,
		"aff_count":         profile.AffCount,
		"aff_quota":         profile.AffQuota,
		"aff_history_quota": profile.AffHistoryQuota,
		"inviter_id":        profile.InviterId,
		"linux_do_id":       profile.LinuxDOId,
		"setting":           profile.Setting,
		"stripe_customer":   profile.StripeCustomer,
		"sidebar_modules":   sidebarModules, // Корректно извлекаем поле sidebar_modules.
		"permissions":       permissions,
	}
}

// BuildSelfUserData строит безопасный DTO и сохраняет порядок чтения настроек и прав.
func BuildSelfUserData(profile domain.SelfProfile, capabilities func(int, int) any) map[string]any {
	setting := dto.UserSetting{}
	if profile.Setting != "" {
		err := common.Unmarshal([]byte(profile.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	permissions := domain.DashboardPermissions(profile.Role)
	permissions["admin_permissions"] = capabilities(profile.Id, profile.Role)
	return SelfUserData(profile, setting.SidebarModules, permissions)
}

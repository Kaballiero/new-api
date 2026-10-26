package dto

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

// SelfUserData builds the safe profile response of the dashboard, login and session.
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
		"sidebar_modules":   sidebarModules,
		"permissions":       permissions,
	}
}

// BuildSelfUserData builds the safe profile DTO and keeps the order of reading
// settings and permissions.
func BuildSelfUserData(profile domain.SelfProfile, sidebar func(string) string, capabilities func(int, int) any, roles domain.AdminRoles) map[string]any {
	sidebarModules := sidebar(profile.Setting)
	permissions := domain.DashboardPermissions(profile.Role, roles)
	permissions["admin_permissions"] = capabilities(profile.Id, profile.Role)
	return SelfUserData(profile, sidebarModules, permissions)
}

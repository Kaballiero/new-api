package domain

import (
	"fmt"
	"os"
)

var auditContentTemplates = map[string]string{
	"user.create":               "Created user ${username} (role ${role})",
	"user.update":               "Updated user ${username} (ID: ${id})",
	"user.delete":               "Deleted user ${username} (ID: ${id})",
	"user.account_delete":       "Account deletion",
	"user.manage":               "Performed ${action} on user ${username} (ID: ${id})",
	"user.quota_add":            "Increased user quota by ${quota}",
	"user.quota_subtract":       "Decreased user quota by ${quota}",
	"user.quota_override":       "Overrode user quota from ${from} to ${to}",
	"user.binding_clear":        "Cleared ${bindingType} binding for user ${username}",
	"user.2fa_disable":          "Force-disabled two-factor authentication for the user",
	"user.passkey_register":     "Registered a passkey",
	"access_token.generate":     "Generated a system access token",
	"access_token.revoke":       "Revoked the system access token",
	"user.2fa_setup":            "Started two-factor authentication setup",
	"user.2fa_enable":           "Enabled two-factor authentication",
	"user.2fa_disable_self":     "Disabled two-factor authentication",
	"user.2fa_backup_codes":     "Regenerated two-factor backup codes",
	"user.security_verify":      "Completed security verification",
	"user.password_change":      "Account password change",
	"user.binding_start":        "Account binding request",
	"user.binding_bind":         "Account binding",
	"user.binding_unbind":       "Account unlinking",
	"user.email_binding_resend": "Email confirmation code resend",
	"user.passkey_delete":       "Deleted a passkey",
	"user.reset_passkey":        "Reset the user passkey",
	"option.update":             "Updated system setting ${key}",

	"channel.create":             "Created channel ${name} (type ${type}, count ${count})",
	"channel.update":             "Updated channel ${name} (ID: ${id})",
	"channel.delete":             "Deleted channel ${name} (ID: ${id})",
	"channel.delete_batch":       "Batch deleted ${count} channels",
	"channel.delete_disabled":    "Deleted all disabled channels (${count})",
	"channel.key_view":           "Viewed channel key ${name} (ID: ${id})",
	"channel.tag_disable":        "Disabled channels with tag ${tag}",
	"channel.tag_enable":         "Enabled channels with tag ${tag}",
	"channel.tag_edit":           "Edited channels with tag ${tag}",
	"channel.tag_batch_set":      "Batch set tag for ${count} channels",
	"channel.copy":               "Copied channel (source ID: ${sourceId}) to ${name} (new ID: ${id})",
	"channel.multi_key_manage":   "Multi-key management ${action} on channel (ID: ${id})",
	"channel.upstream_apply":     "Applied upstream model changes to channel (ID: ${id})",
	"channel.upstream_apply_all": "Applied upstream model changes to ${count} channels",

	"redemption.create":       "Created ${count} redemption codes named ${name} (${quota} each)",
	"redemption.delete_batch": "Batch deleted ${count} redemption codes",

	"subscription.plan_reset":      "Reset active subscriptions for plan ${plan_id}",
	"subscription.user_plan_reset": "Reset active plan ${plan_id} subscriptions for user ${target_user_id}",
}

func OperationContent(action string, params map[string]any) string {
	tmpl, ok := auditContentTemplates[action]
	if !ok {
		return action
	}
	return os.Expand(tmpl, func(key string) string {
		if v, ok := params[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	})
}

package settings

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	artifacts "github.com/QuantumNous/new-api/internal/adapter/artifacts"
	"github.com/QuantumNous/new-api/internal/settings/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func (Validation) ComplianceConfirmed() bool { return operation_setting.IsPaymentComplianceConfirmed() }
func (Validation) TaskPublicAddress(value string) error {
	return artifacts.ValidateTaskArtifactBaseURL(value)
}
func (Validation) RequirementSatisfied(key string) bool {
	switch key {
	case "GitHubOAuthEnabled":
		return common.GitHubClientId != ""
	case "discord.enabled":
		return system_setting.GetDiscordSettings().ClientId != ""
	case "oidc.enabled":
		return system_setting.GetOIDCSettings().ClientId != ""
	case "LinuxDOOAuthEnabled":
		return common.LinuxDOClientId != ""
	case "EmailDomainRestrictionEnabled":
		return len(common.EmailDomainWhitelist) != 0
	case "WeChatAuthEnabled":
		return common.WeChatServerAddress != ""
	case "TurnstileCheckEnabled":
		return common.TurnstileSiteKey != ""
	case "TelegramOAuthEnabled":
		return system_setting.GetTelegramSettings().IsConfigured()
	}
	return true
}
func (Validation) Check(key, value string) error {
	var err error
	switch key {
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(value)
	case "gemini.safety_settings":
		err = model_setting.ValidateGeminiSafetySettings(value)
	case "claude.default_max_tokens":
		err = model_setting.ValidateClaudeDefaultMaxTokens(value)
	case operation_setting.ToolPriceOptionKey:
		err = operation_setting.ValidateToolPricesJSON(value)
	case "ImageRatio":
		err = ratio_setting.UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		err = ratio_setting.UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		err = ratio_setting.UpdateAudioCompletionRatioByJSONString(value)
	case "CreateCacheRatio":
		err = ratio_setting.UpdateCreateCacheRatioByJSONString(value)
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(value)
	case "AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(value)
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(value, "ApiInfo")
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(value, "Announcements")
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(value, "FAQ")
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(value, "UptimeKumaGroups")
	case "billing_setting.batch_billing_expr":
		var expressions map[string]string
		if err := common.UnmarshalJsonStr(value, &expressions); err != nil || expressions == nil {
			return &domain.InvalidOption{Kind: "batch_object"}
		}
		for name, expression := range expressions {
			if err := billing_setting.SmokeTestExpr(expression); err != nil {
				return &domain.InvalidOption{Kind: "batch_expression", Model: name, Cause: err}
			}
		}
	case "billing_setting.billing_expr":
		expressions := make(map[string]string)
		if err := common.UnmarshalJsonStr(value, &expressions); err != nil {
			return &domain.InvalidOption{Kind: "expression_object", Cause: err}
		}
		models := make([]string, 0, len(expressions))
		for name := range expressions {
			models = append(models, name)
		}
		sort.Strings(models)
		generation := jsplugin.DefaultRegistry.Generation()
		for _, name := range models {
			expression := expressions[name]
			if plugin, ok := generation.GetByModel(name); ok {
				err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
			} else if target, resolved := model.ResolveTaskModelAlias(generation, name); resolved {
				if plugin, ok := generation.Get(target.PluginKey); ok {
					err = billing_setting.SmokeTestTaskExpr(expression, plugin.Meta.UsageSchema)
				} else {
					err = billing_setting.SmokeTestExpr(expression)
				}
			} else {
				err = billing_setting.SmokeTestExpr(expression)
			}
			if err != nil {
				return &domain.InvalidOption{Kind: "model_expression", Model: name, Cause: err}
			}
		}
	}
	if err != nil {
		switch key {
		case "ImageRatio", "AudioRatio", "AudioCompletionRatio", "CreateCacheRatio":
			return &domain.InvalidOption{Kind: key, Cause: err}
		}
	}
	return err
}

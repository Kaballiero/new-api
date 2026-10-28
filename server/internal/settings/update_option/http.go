package update_option

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/settings/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	usecase Usecase
	audit   func(*gin.Context, string)
}

func NewHandler(usecase Usecase, audit func(*gin.Context, string)) Handler {
	return Handler{usecase, audit}
}
func (h Handler) Handle(c *gin.Context) {
	var option OptionUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &option); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": optionMessage(c, i18n.MsgSettingsInvalidParameters)})
		return
	}
	switch option.Value.(type) {
	case bool:
		option.Value = common.Interface2String(option.Value.(bool))
	case float64:
		option.Value = common.Interface2String(option.Value.(float64))
	case int:
		option.Value = common.Interface2String(option.Value.(int))
	default:
		option.Value = fmt.Sprintf("%v", option.Value)
	}
	if err := h.usecase.Execute(option.Key, option.Value.(string)); err != nil {
		if errors.Is(err, domain.ErrComplianceRequired) {
			common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
			return
		}
		message := err.Error()
		switch {
		case errors.Is(err, domain.ErrComplianceField):
			message = optionMessage(c, i18n.MsgSettingsComplianceField)
		case errors.Is(err, domain.ErrRetiredTheme):
			message = optionMessage(c, i18n.MsgSettingsRetiredTheme)
		}
		var invalid *domain.InvalidOption
		if errors.As(err, &invalid) {
			switch invalid.Kind {
			case "GitHubOAuthEnabled":
				message = optionMessage(c, i18n.MsgSettingsGitHub)
			case "discord.enabled":
				message = optionMessage(c, i18n.MsgSettingsDiscord)
			case "oidc.enabled":
				message = optionMessage(c, i18n.MsgSettingsOIDC)
			case "LinuxDOOAuthEnabled":
				message = optionMessage(c, i18n.MsgSettingsLinuxDO)
			case "EmailDomainRestrictionEnabled":
				message = optionMessage(c, i18n.MsgSettingsEmailDomain)
			case "WeChatAuthEnabled":
				message = optionMessage(c, i18n.MsgSettingsWeChat)
			case "TurnstileCheckEnabled":
				message = optionMessage(c, i18n.MsgSettingsTurnstile)
			case "TelegramOAuthEnabled":
				c.JSON(http.StatusOK, gin.H{"success": false, "code": "TELEGRAM_OAUTH_NOT_CONFIGURED",
					"message": optionMessage(c, i18n.MsgSettingsTelegram)})
				return
			case "ImageRatio":
				message = optionMessage(c, i18n.MsgSettingsImageRatio, map[string]any{"Cause": invalid.Cause.Error()})
			case "AudioRatio":
				message = optionMessage(c, i18n.MsgSettingsAudioRatio, map[string]any{"Cause": invalid.Cause.Error()})
			case "AudioCompletionRatio":
				message = optionMessage(c, i18n.MsgSettingsAudioCompletionRatio, map[string]any{"Cause": invalid.Cause.Error()})
			case "CreateCacheRatio":
				message = optionMessage(c, i18n.MsgSettingsCreateCacheRatio, map[string]any{"Cause": invalid.Cause.Error()})
			case "batch_object":
				message = optionMessage(c, i18n.MsgSettingsBatchObject)
			case "batch_expression":
				message = optionMessage(c, i18n.MsgSettingsBatchExpression, map[string]any{"Cause": fmt.Sprint(invalid.Cause), "Model": invalid.Model})
			case "expression_object":
				message = optionMessage(c, i18n.MsgSettingsExpressionObject, map[string]any{"Cause": invalid.Cause.Error()})
			case "model_expression":
				message = optionMessage(c, i18n.MsgSettingsModelExpression, map[string]any{"Cause": fmt.Sprint(invalid.Cause), "Model": invalid.Model})
			}
		}
		common.ApiErrorMsg(c, message)
		return
	}
	// Audit only the key; option values may contain credentials.
	h.audit(c, option.Key)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// optionMessage resolves request language only when the request is available.
func optionMessage(c *gin.Context, key string, args ...map[string]any) string {
	language := i18n.DefaultLang
	if c != nil && c.Request != nil {
		language = i18n.GetLangFromContext(c)
	}
	return i18n.Translate(language, key, args...)
}

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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的参数"})
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
			message = "合规确认字段不允许通过通用设置接口修改"
		case errors.Is(err, domain.ErrRetiredTheme):
			message = "Classic 前端已移除，主题只能设置为 default"
		}
		var invalid *domain.InvalidOption
		if errors.As(err, &invalid) {
			switch invalid.Kind {
			case "GitHubOAuthEnabled":
				message = "无法启用 GitHub OAuth，请先填入 GitHub Client Id 以及 GitHub Client Secret！"
			case "discord.enabled":
				message = "无法启用 Discord OAuth，请先填入 Discord Client Id 以及 Discord Client Secret！"
			case "oidc.enabled":
				message = "无法启用 OIDC 登录，请先填入 OIDC Client Id 以及 OIDC Client Secret！"
			case "LinuxDOOAuthEnabled":
				message = "无法启用 LinuxDO OAuth，请先填入 LinuxDO Client Id 以及 LinuxDO Client Secret！"
			case "EmailDomainRestrictionEnabled":
				message = "无法启用邮箱域名限制，请先填入限制的邮箱域名！"
			case "WeChatAuthEnabled":
				message = "无法启用微信登录，请先填入微信登录相关配置信息！"
			case "TurnstileCheckEnabled":
				message = "无法启用 Turnstile 校验，请先填入 Turnstile 校验相关配置信息！"
			case "TelegramOAuthEnabled":
				c.JSON(http.StatusOK, gin.H{"success": false, "code": "TELEGRAM_OAUTH_NOT_CONFIGURED",
					"message": "Telegram OAuth is not configured or enabled. Please contact your administrator."})
				return
			case "ImageRatio":
				message = "图片倍率设置失败: " + invalid.Cause.Error()
			case "AudioRatio":
				message = "音频倍率设置失败: " + invalid.Cause.Error()
			case "AudioCompletionRatio":
				message = "音频补全倍率设置失败: " + invalid.Cause.Error()
			case "CreateCacheRatio":
				message = "缓存创建倍率设置失败: " + invalid.Cause.Error()
			case "batch_object":
				message = "batch billing expressions must be a JSON object"
			case "batch_expression":
				message = fmt.Sprintf("invalid batch expression for %s: %v", invalid.Model, invalid.Cause)
			case "expression_object":
				message = "计费表达式配置必须是模型到表达式的 JSON 对象: " + invalid.Cause.Error()
			case "model_expression":
				message = fmt.Sprintf("模型 %s 的计费表达式无效: %v", invalid.Model, invalid.Cause)
			}
		}
		common.ApiErrorMsg(c, message)
		return
	}
	// Audit only the key; option values may contain credentials.
	h.audit(c, option.Key)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

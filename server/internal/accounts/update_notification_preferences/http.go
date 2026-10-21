package update_notification_preferences

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(store NotificationPreferencesStore) *HTTP {
	return &HTTP{scenario: NewUsecase(store)}
}

type notificationPreferencesUser struct{ context *gin.Context }

func (user notificationPreferencesUser) UserID() int { return user.context.GetInt("id") }

func (h *HTTP) Handle(c *gin.Context) {
	var req UpdateUserSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	requested := dto.UserSetting{
		NotifyType:            req.QuotaWarningType,
		QuotaWarningThreshold: req.QuotaWarningThreshold,
		WebhookUrl:            req.WebhookUrl, WebhookSecret: req.WebhookSecret,
		NotificationEmail: req.NotificationEmail,
		BarkUrl:           req.BarkUrl,
		GotifyUrl:         req.GotifyUrl, GotifyToken: req.GotifyToken, GotifyPriority: req.GotifyPriority,
		AcceptUnsetRatioModel: req.AcceptUnsetModelRatioModel, RecordIpLog: req.RecordIpLog,
	}
	if err := h.scenario.Execute(requested, req.UpstreamModelUpdateNotifyEnabled, notificationPreferencesUser{context: c}); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
}

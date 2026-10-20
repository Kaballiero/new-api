package update_notification_preferences

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
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
	failure := h.scenario.Execute(requested, req.UpstreamModelUpdateNotifyEnabled, notificationPreferencesUser{context: c})
	if failure == nil {
		common.ApiSuccessI18n(c, i18n.MsgSettingSaved, nil)
		return
	}
	switch failure.Issue {
	case NotificationSettingInvalidType:
		common.ApiErrorI18n(c, i18n.MsgSettingInvalidType)
		return
	case NotificationQuotaThresholdGtZero:
		common.ApiErrorI18n(c, i18n.MsgQuotaThresholdGtZero)
		return
	case NotificationSettingWebhookEmpty:
		common.ApiErrorI18n(c, i18n.MsgSettingWebhookEmpty)
		return
	case NotificationSettingWebhookInvalid:
		common.ApiErrorI18n(c, i18n.MsgSettingWebhookInvalid)
		return
	case NotificationSettingEmailInvalid:
		common.ApiErrorI18n(c, i18n.MsgSettingEmailInvalid)
		return
	case NotificationSettingBarkUrlEmpty:
		common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlEmpty)
		return
	case NotificationSettingBarkUrlInvalid:
		common.ApiErrorI18n(c, i18n.MsgSettingBarkUrlInvalid)
		return
	case NotificationSettingUrlMustHttp:
		common.ApiErrorI18n(c, i18n.MsgSettingUrlMustHttp)
		return
	case NotificationSettingGotifyUrlEmpty:
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlEmpty)
		return
	case NotificationSettingGotifyTokenEmpty:
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyTokenEmpty)
		return
	case NotificationSettingGotifyUrlInvalid:
		common.ApiErrorI18n(c, i18n.MsgSettingGotifyUrlInvalid)
		return
	}

	if failure.Saving {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}
	common.ApiError(c, failure.Err)
}

package billing

import (
	"github.com/QuantumNous/new-api/i18n"
	notification "github.com/QuantumNous/new-api/internal/adapter/notifications"
	payments "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	notificationdomain "github.com/QuantumNous/new-api/internal/notifications/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type Notification struct {
	Send func(int, string, notificationdomain.Preferences, notificationdomain.Notice) error
}

func (n Notification) Deliver(notice domain.Notice) error {
	r := notice.Recipient
	language := r.Language
	if language == "" {
		language = i18n.DefaultLang
	}
	titleKey := i18n.MsgBillingWalletWarning
	if notice.Subscription {
		titleKey = i18n.MsgBillingSubscriptionWarning
	}
	title := i18n.Translate(language, titleKey)
	bodyKey := i18n.MsgBillingQuotaWarningHTML
	if r.NotifyType == dto.NotifyTypeBark {
		bodyKey = i18n.MsgBillingQuotaWarningBrief
	}
	if r.NotifyType == dto.NotifyTypeGotify {
		bodyKey = i18n.MsgBillingQuotaWarningText
	}
	values := []any{title, logger.FormatQuota(notice.Remaining)}
	if bodyKey == i18n.MsgBillingQuotaWarningHTML {
		link := payments.PaymentReturnURL("/wallet")
		values = append(values, link, link)
	}
	// Preserve the legacy template and Values wire contract, including the
	// existing webhook renderer. Localization must not silently repair it.
	content := i18n.Translate(language, bodyKey, map[string]any{"Title": dto.ContentValueParam, "Remaining": dto.ContentValueParam, "URL": dto.ContentValueParam})
	settings := dto.UserSetting{NotifyType: r.NotifyType, Language: r.Language, QuotaWarningThreshold: r.WarningThreshold, NotificationEmail: r.NotificationEmail, WebhookUrl: r.WebhookURL, WebhookSecret: r.WebhookSecret, BarkUrl: r.BarkURL, GotifyUrl: r.GotifyURL, GotifyToken: r.GotifyToken, GotifyPriority: r.GotifyPriority}
	return n.Send(r.UserID, r.Email, notification.Preferences(settings), notification.Notice(dto.NewNotify(dto.NotifyTypeQuotaExceed, title, content, values)))
}

package billing

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
)

type Notification struct{}

func (Notification) Deliver(notice domain.Notice) error {
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
		link := service.PaymentReturnURL("/wallet")
		values = append(values, link, link)
	}
	// Preserve the legacy template and Values wire contract, including the
	// existing webhook renderer. Localization must not silently repair it.
	content := i18n.Translate(language, bodyKey, map[string]any{"Title": dto.ContentValueParam, "Remaining": dto.ContentValueParam, "URL": dto.ContentValueParam})
	settings := dto.UserSetting{NotifyType: r.NotifyType, Language: r.Language, QuotaWarningThreshold: r.WarningThreshold, NotificationEmail: r.NotificationEmail, WebhookUrl: r.WebhookURL, WebhookSecret: r.WebhookSecret, BarkUrl: r.BarkURL, GotifyUrl: r.GotifyURL, GotifyToken: r.GotifyToken, GotifyPriority: r.GotifyPriority}
	return service.NotifyUser(r.UserID, r.Email, settings, dto.NewNotify(dto.NotifyTypeQuotaExceed, title, content, values))
}

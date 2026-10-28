package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func settingsFromWire(settings dto.UserSetting) domain.UserSetting {
	return domain.UserSetting{
		NotifyType:                       settings.NotifyType,
		QuotaWarningThreshold:            settings.QuotaWarningThreshold,
		WebhookUrl:                       settings.WebhookUrl,
		WebhookSecret:                    settings.WebhookSecret,
		NotificationEmail:                settings.NotificationEmail,
		BarkUrl:                          settings.BarkUrl,
		GotifyUrl:                        settings.GotifyUrl,
		GotifyToken:                      settings.GotifyToken,
		GotifyPriority:                   settings.GotifyPriority,
		UpstreamModelUpdateNotifyEnabled: settings.UpstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            settings.AcceptUnsetRatioModel,
		RecordIpLog:                      settings.RecordIpLog,
		SidebarModules:                   settings.SidebarModules,
		BillingPreference:                settings.BillingPreference,
		Language:                         settings.Language,
	}
}

func settingsToWire(settings domain.UserSetting) dto.UserSetting {
	return dto.UserSetting{
		NotifyType:                       settings.NotifyType,
		QuotaWarningThreshold:            settings.QuotaWarningThreshold,
		WebhookUrl:                       settings.WebhookUrl,
		WebhookSecret:                    settings.WebhookSecret,
		NotificationEmail:                settings.NotificationEmail,
		BarkUrl:                          settings.BarkUrl,
		GotifyUrl:                        settings.GotifyUrl,
		GotifyToken:                      settings.GotifyToken,
		GotifyPriority:                   settings.GotifyPriority,
		UpstreamModelUpdateNotifyEnabled: settings.UpstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            settings.AcceptUnsetRatioModel,
		RecordIpLog:                      settings.RecordIpLog,
		SidebarModules:                   settings.SidebarModules,
		BillingPreference:                settings.BillingPreference,
		Language:                         settings.Language,
	}
}

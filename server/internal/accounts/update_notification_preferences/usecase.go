package update_notification_preferences

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"net/url"
	"strings"
)

type NotificationSettingsIssue uint8

const (
	NotificationSettingsValid NotificationSettingsIssue = iota
	NotificationSettingInvalidType
	NotificationQuotaThresholdGtZero
	NotificationSettingWebhookEmpty
	NotificationSettingWebhookInvalid
	NotificationSettingEmailInvalid
	NotificationSettingBarkUrlEmpty
	NotificationSettingBarkUrlInvalid
	NotificationSettingUrlMustHttp
	NotificationSettingGotifyUrlEmpty
	NotificationSettingGotifyTokenEmpty
	NotificationSettingGotifyUrlInvalid
)

func ValidateNotificationSettings(requested dto.UserSetting) NotificationSettingsIssue {
	// Проверяем тип предупреждения.
	if requested.NotifyType != dto.NotifyTypeEmail && requested.NotifyType != dto.NotifyTypeWebhook && requested.NotifyType != dto.NotifyTypeBark && requested.NotifyType != dto.NotifyTypeGotify {
		return NotificationSettingInvalidType
	}

	// Проверяем порог предупреждения.
	if requested.QuotaWarningThreshold <= 0 {
		return NotificationQuotaThresholdGtZero
	}

	// Для webhook проверяем адрес получателя.
	if requested.NotifyType == dto.NotifyTypeWebhook {
		if requested.WebhookUrl == "" {
			return NotificationSettingWebhookEmpty
		}
		// Проверяем формат URL.
		if _, err := url.ParseRequestURI(requested.WebhookUrl); err != nil {
			return NotificationSettingWebhookInvalid
		}
	}

	// Для уведомлений по почте проверяем адрес электронной почты.
	if requested.NotifyType == dto.NotifyTypeEmail && requested.NotificationEmail != "" {
		// Проверяем формат адреса электронной почты.
		if !strings.Contains(requested.NotificationEmail, "@") {
			return NotificationSettingEmailInvalid
		}
	}

	// Для Bark проверяем URL.
	if requested.NotifyType == dto.NotifyTypeBark {
		if requested.BarkUrl == "" {
			return NotificationSettingBarkUrlEmpty
		}
		// Проверяем формат URL.
		if _, err := url.ParseRequestURI(requested.BarkUrl); err != nil {
			return NotificationSettingBarkUrlInvalid
		}
		// Проверяем, используется ли HTTP или HTTPS.
		if !strings.HasPrefix(requested.BarkUrl, "https://") && !strings.HasPrefix(requested.BarkUrl, "http://") {
			return NotificationSettingUrlMustHttp
		}
	}

	// Для Gotify проверяем URL и Token.
	if requested.NotifyType == dto.NotifyTypeGotify {
		if requested.GotifyUrl == "" {
			return NotificationSettingGotifyUrlEmpty
		}
		if requested.GotifyToken == "" {
			return NotificationSettingGotifyTokenEmpty
		}
		// Проверяем формат URL.
		if _, err := url.ParseRequestURI(requested.GotifyUrl); err != nil {
			return NotificationSettingGotifyUrlInvalid
		}
		// Проверяем, используется ли HTTP или HTTPS.
		if !strings.HasPrefix(requested.GotifyUrl, "https://") && !strings.HasPrefix(requested.GotifyUrl, "http://") {
			return NotificationSettingUrlMustHttp
		}
	}

	return NotificationSettingsValid
}

func NotificationSettings(requested, existingSettings dto.UserSetting, role int, upstreamRequested *bool) dto.UserSetting {
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if role >= common.RoleAdminUser && upstreamRequested != nil {
		upstreamModelUpdateNotifyEnabled = *upstreamRequested
	}

	// Формируем настройки.
	settings := dto.UserSetting{
		NotifyType:                       requested.NotifyType,
		QuotaWarningThreshold:            requested.QuotaWarningThreshold,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            requested.AcceptUnsetRatioModel,
		RecordIpLog:                      requested.RecordIpLog,
	}

	// Для webhook добавляем соответствующие настройки.
	if requested.NotifyType == dto.NotifyTypeWebhook {
		settings.WebhookUrl = requested.WebhookUrl
		if requested.WebhookSecret != "" {
			settings.WebhookSecret = requested.WebhookSecret
		}
	}

	// Если указан адрес для уведомлений по почте, добавляем его в настройки.
	if requested.NotifyType == dto.NotifyTypeEmail && requested.NotificationEmail != "" {
		settings.NotificationEmail = requested.NotificationEmail
	}

	// Для Bark добавляем URL в настройки.
	if requested.NotifyType == dto.NotifyTypeBark {
		settings.BarkUrl = requested.BarkUrl
	}

	// Для Gotify добавляем его конфигурацию в настройки.
	if requested.NotifyType == dto.NotifyTypeGotify {
		settings.GotifyUrl = requested.GotifyUrl
		settings.GotifyToken = requested.GotifyToken
		// Приоритет Gotify должен быть от 0 до 10; вне диапазона используем значение по умолчанию 5.
		if requested.GotifyPriority < 0 || requested.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = requested.GotifyPriority
		}
	}

	return settings
}

// NotificationPreferencesStore скрывает способ чтения аккаунта и сохранения настроек.
type NotificationPreferencesStore interface {
	NotificationPreferencesAccount(id int) (NotificationPreferencesAccount, error)
	SaveNotificationPreferences(id int, settings dto.UserSetting) error
}

// NotificationPreferencesUser откладывает чтение ID до успешной проверки настроек.
type NotificationPreferencesUser interface{ UserID() int }

type Usecase struct{ Store NotificationPreferencesStore }

func (preferences Usecase) Execute(requested dto.UserSetting, upstreamRequested *bool, user NotificationPreferencesUser) *NotificationPreferencesFailure {
	if issue := ValidateNotificationSettings(requested); issue != NotificationSettingsValid {
		return &NotificationPreferencesFailure{Issue: issue}
	}
	account, err := preferences.Store.NotificationPreferencesAccount(user.UserID())
	if err != nil {
		return &NotificationPreferencesFailure{Err: err}
	}
	settings := NotificationSettings(requested, account.Settings, account.Role, upstreamRequested)
	if err := preferences.Store.SaveNotificationPreferences(account.ID, settings); err != nil {
		return &NotificationPreferencesFailure{Err: err, Saving: true}
	}
	return nil
}

func NewUsecase(store NotificationPreferencesStore) Usecase {
	return Usecase{Store: store}
}

package update_notification_preferences

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

// ValidateNotificationSettings returns the domain error of the first invalid
// notification setting, or nil.
func ValidateNotificationSettings(requested domain.UserSetting, types NotificationTypes) error {
	// Check the warning type.
	if requested.NotifyType != types.Email && requested.NotifyType != types.Webhook && requested.NotifyType != types.Bark && requested.NotifyType != types.Gotify {
		return domain.ErrNotifyTypeInvalid
	}

	// Check the warning threshold.
	if requested.QuotaWarningThreshold <= 0 {
		return domain.ErrQuotaThresholdNotPositive
	}

	// For webhooks check the receiver URL.
	if requested.NotifyType == types.Webhook {
		if requested.WebhookUrl == "" {
			return domain.ErrWebhookURLEmpty
		}
		// Check the URL format.
		if _, err := url.ParseRequestURI(requested.WebhookUrl); err != nil {
			return domain.ErrWebhookURLInvalid
		}
	}

	// For email notifications check the email address.
	if requested.NotifyType == types.Email && requested.NotificationEmail != "" {
		// Check the email address format.
		if !strings.Contains(requested.NotificationEmail, "@") {
			return domain.ErrNotificationEmailInvalid
		}
	}

	// For Bark check the URL.
	if requested.NotifyType == types.Bark {
		if requested.BarkUrl == "" {
			return domain.ErrBarkURLEmpty
		}
		// Check the URL format.
		if _, err := url.ParseRequestURI(requested.BarkUrl); err != nil {
			return domain.ErrBarkURLInvalid
		}
		// Check that HTTP or HTTPS is used.
		if !strings.HasPrefix(requested.BarkUrl, "https://") && !strings.HasPrefix(requested.BarkUrl, "http://") {
			return domain.ErrNotificationURLNotHTTP
		}
	}

	// For Gotify check the URL and the token.
	if requested.NotifyType == types.Gotify {
		if requested.GotifyUrl == "" {
			return domain.ErrGotifyURLEmpty
		}
		if requested.GotifyToken == "" {
			return domain.ErrGotifyTokenEmpty
		}
		// Check the URL format.
		if _, err := url.ParseRequestURI(requested.GotifyUrl); err != nil {
			return domain.ErrGotifyURLInvalid
		}
		// Check that HTTP or HTTPS is used.
		if !strings.HasPrefix(requested.GotifyUrl, "https://") && !strings.HasPrefix(requested.GotifyUrl, "http://") {
			return domain.ErrNotificationURLNotHTTP
		}
	}

	return nil
}

func NotificationSettings(requested, existingSettings domain.UserSetting, role, adminRole int, upstreamRequested *bool, types NotificationTypes) domain.UserSetting {
	upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
	if role >= adminRole && upstreamRequested != nil {
		upstreamModelUpdateNotifyEnabled = *upstreamRequested
	}

	// Build the settings.
	settings := domain.UserSetting{
		NotifyType:                       requested.NotifyType,
		QuotaWarningThreshold:            requested.QuotaWarningThreshold,
		UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
		AcceptUnsetRatioModel:            requested.AcceptUnsetRatioModel,
		RecordIpLog:                      requested.RecordIpLog,
	}

	// For webhooks add the webhook settings.
	if requested.NotifyType == types.Webhook {
		settings.WebhookUrl = requested.WebhookUrl
		if requested.WebhookSecret != "" {
			settings.WebhookSecret = requested.WebhookSecret
		}
	}

	// Add the notification email when it is set.
	if requested.NotifyType == types.Email && requested.NotificationEmail != "" {
		settings.NotificationEmail = requested.NotificationEmail
	}

	// For Bark add the URL.
	if requested.NotifyType == types.Bark {
		settings.BarkUrl = requested.BarkUrl
	}

	// For Gotify add its configuration.
	if requested.NotifyType == types.Gotify {
		settings.GotifyUrl = requested.GotifyUrl
		settings.GotifyToken = requested.GotifyToken
		// The Gotify priority must be 0..10; out-of-range values fall back to 5.
		if requested.GotifyPriority < 0 || requested.GotifyPriority > 10 {
			settings.GotifyPriority = 5
		} else {
			settings.GotifyPriority = requested.GotifyPriority
		}
	}

	return settings
}

// NotificationPreferencesStore hides how the account is read and the settings are saved.
type NotificationPreferencesStore interface {
	NotificationPreferencesAccount(id int) (NotificationPreferencesAccount, error)
	SaveNotificationPreferences(id int, settings domain.UserSetting) error
}

// NotificationPreferencesUser defers reading the ID until the settings are validated.
type NotificationPreferencesUser interface{ UserID() int }

type Usecase struct {
	store     NotificationPreferencesStore
	types     func() NotificationTypes
	adminRole int
}

// Execute validates and saves the notification settings; a load failure is
// returned unclassified.
func (preferences Usecase) Execute(requested domain.UserSetting, upstreamRequested *bool, user NotificationPreferencesUser) error {
	if err := ValidateNotificationSettings(requested, preferences.types()); err != nil {
		return err
	}
	account, err := preferences.store.NotificationPreferencesAccount(user.UserID())
	if err != nil {
		return err
	}
	settings := NotificationSettings(requested, account.Settings, account.Role, preferences.adminRole, upstreamRequested, preferences.types())
	if err := preferences.store.SaveNotificationPreferences(account.ID, settings); err != nil {
		return domain.SettingsSaveFailed(err)
	}
	return nil
}

func NewUsecase(store NotificationPreferencesStore, types func() NotificationTypes, adminRole int) Usecase {
	return Usecase{store: store, types: types, adminRole: adminRole}
}

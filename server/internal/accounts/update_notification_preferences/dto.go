package update_notification_preferences

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// NotificationPreferencesAccount содержит данные, необходимые для смены настроек уведомлений.
type NotificationPreferencesAccount struct {
	ID       int
	Role     int
	Settings dto.UserSetting
}

// NotificationPreferencesFailure сохраняет различия ошибок проверки, чтения и записи.
type NotificationPreferencesFailure struct {
	Issue  NotificationSettingsIssue
	Err    error
	Saving bool
}

type UpdateUserSettingRequest struct {
	QuotaWarningType                 string  `json:"notify_type"`
	QuotaWarningThreshold            float64 `json:"quota_warning_threshold"`
	WebhookUrl                       string  `json:"webhook_url,omitempty"`
	WebhookSecret                    string  `json:"webhook_secret,omitempty"`
	NotificationEmail                string  `json:"notification_email,omitempty"`
	BarkUrl                          string  `json:"bark_url,omitempty"`
	GotifyUrl                        string  `json:"gotify_url,omitempty"`
	GotifyToken                      string  `json:"gotify_token,omitempty"`
	GotifyPriority                   int     `json:"gotify_priority,omitempty"`
	UpstreamModelUpdateNotifyEnabled *bool   `json:"upstream_model_update_notify_enabled,omitempty"`
	AcceptUnsetModelRatioModel       bool    `json:"accept_unset_model_ratio_model"`
	RecordIpLog                      bool    `json:"record_ip_log"`
}

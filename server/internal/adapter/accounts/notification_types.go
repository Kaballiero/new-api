package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// NotificationTypes reads the existing wire identifiers when the scenario needs them.
func NotificationTypes() update_notification_preferences.NotificationTypes {
	return update_notification_preferences.NotificationTypes{
		Email: dto.NotifyTypeEmail, Webhook: dto.NotifyTypeWebhook,
		Bark: dto.NotifyTypeBark, Gotify: dto.NotifyTypeGotify,
	}
}

package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	"github.com/QuantumNous/new-api/model"
)

// NotificationPreferencesStore связывает сценарий с прежними операциями аккаунта.
type NotificationPreferencesStore struct{}

func (NotificationPreferencesStore) NotificationPreferencesAccount(id int) (update_notification_preferences.NotificationPreferencesAccount, error) {
	user, err := model.GetUserById(id, true)
	if err != nil {
		return update_notification_preferences.NotificationPreferencesAccount{}, err
	}
	settings := user.GetSetting()
	return update_notification_preferences.NotificationPreferencesAccount{ID: user.Id, Role: user.Role, Settings: settingsFromWire(settings)}, nil
}

func (NotificationPreferencesStore) SaveNotificationPreferences(id int, settings domain.UserSetting) error {
	return model.UpdateUserSetting(id, settingsToWire(settings))
}

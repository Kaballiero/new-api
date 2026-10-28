package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
)

// NotificationPreferencesStore связывает сценарий с прежними операциями аккаунта.
type NotificationPreferencesStore struct{ SQL core.Store }

func (store NotificationPreferencesStore) NotificationPreferencesAccount(id int) (update_notification_preferences.NotificationPreferencesAccount, error) {
	user, err := store.SQL.GetUserById(id, true)
	if err != nil {
		return update_notification_preferences.NotificationPreferencesAccount{}, err
	}
	settings := user.GetSetting()
	return update_notification_preferences.NotificationPreferencesAccount{ID: user.Id, Role: user.Role, Settings: settingsFromWire(settings)}, nil
}

func (store NotificationPreferencesStore) SaveNotificationPreferences(id int, settings domain.UserSetting) error {
	return store.SQL.UpdateUserSetting(id, settingsToWire(settings))
}

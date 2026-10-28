package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	core "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	"github.com/QuantumNous/new-api/model"
)

// SelfUpdateStore связывает изменение собственного профиля с прежними методами записи.
type SelfUpdateStore struct {
	SQL    core.Store
	Update func(*model.User, bool) error
}

func (store SelfUpdateStore) SelfPreferences(id int) (update_profile.SelfPreferencesAccount, error) {
	user, err := store.SQL.GetUserById(id, false)
	if err != nil {
		return update_profile.SelfPreferencesAccount{}, err
	}
	settings := user.GetSetting()
	return update_profile.SelfPreferencesAccount{ID: user.Id, Settings: settingsFromWire(settings)}, nil
}

func (store SelfUpdateStore) SaveSelfPreferences(id int, settings domain.UserSetting) error {
	return store.SQL.UpdateUserSetting(id, settingsToWire(settings))
}

func (store SelfUpdateStore) UpdateSelfAccount(account update_profile.SelfUpdateAccount) error {
	user := model.User{Id: account.ID, Username: account.Username, Password: account.Password, DisplayName: account.DisplayName}
	return store.Update(&user, false)
}

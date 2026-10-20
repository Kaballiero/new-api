package accountsadapter

import (
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// SelfUpdateStore связывает изменение собственного профиля с прежними методами записи.
type SelfUpdateStore struct{}

func (SelfUpdateStore) SelfPreferences(id int) (update_profile.SelfPreferencesAccount, error) {
	user, err := model.GetUserById(id, false)
	if err != nil {
		return update_profile.SelfPreferencesAccount{}, err
	}
	settings := user.GetSetting()
	return update_profile.SelfPreferencesAccount{ID: user.Id, Settings: settings}, nil
}

func (SelfUpdateStore) SaveSelfPreferences(id int, settings dto.UserSetting) error {
	return model.UpdateUserSetting(id, settings)
}

func (SelfUpdateStore) UpdateSelfAccount(account update_profile.SelfUpdateAccount) error {
	user := model.User{Id: account.ID, Username: account.Username, Password: account.Password, DisplayName: account.DisplayName}
	return user.Update(false)
}

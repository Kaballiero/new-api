package accountshttp

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	"github.com/QuantumNous/new-api/model"
)

// ProfileInput сохраняет прежнюю проверку полного User, включая неиспользуемые поля запроса.
type ProfileInput struct{}

func (ProfileInput) DecodeAccount(request map[string]any) (update_profile.AccountInput, update_profile.SelfUpdateIssue) {
	var user model.User
	raw, err := common.Marshal(request)
	if err != nil {
		return update_profile.AccountInput{}, update_profile.InvalidParameters
	}
	if err := common.Unmarshal(raw, &user); err != nil {
		return update_profile.AccountInput{}, update_profile.InvalidParameters
	}
	if err := common.Validate.StructExcept(&user, "Password"); err != nil {
		return update_profile.AccountInput{}, update_profile.InvalidInput
	}
	return update_profile.AccountInput{Username: user.Username, Password: user.Password, DisplayName: user.DisplayName, OriginalPassword: user.OriginalPassword}, update_profile.SelfUpdateValid
}

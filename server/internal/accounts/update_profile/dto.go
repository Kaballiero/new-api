package update_profile

import "github.com/QuantumNous/new-api/relaykit/dto"

// SelfUpdateAccount содержит только поля обычного изменения личного профиля.
type SelfUpdateAccount struct {
	ID          int
	Username    string
	Password    string
	DisplayName string
}

// SelfPreferencesAccount содержит текущие настройки без записи хранилища.
type SelfPreferencesAccount struct {
	ID       int
	Settings dto.UserSetting
}

type SelfUpdateResult struct {
	Preferences bool
	Issue       SelfUpdateIssue
	Err         error
}

type AccountInput struct{ Username, Password, DisplayName, OriginalPassword string }

type PasswordResult struct {
	NotificationFailed, ProofRejected bool
	Data                              any
}

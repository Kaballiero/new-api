package update_profile

import "github.com/QuantumNous/new-api/relaykit/dto"

// SelfUpdateAccount holds only the fields of an ordinary own profile update.
type SelfUpdateAccount struct {
	ID          int
	Username    string
	Password    string
	DisplayName string
}

// SelfPreferencesAccount holds the current settings without the stored record.
type SelfPreferencesAccount struct {
	ID       int
	Settings dto.UserSetting
}

// SelfUpdateResult reports what a successful self update changed.
type SelfUpdateResult struct {
	Preferences bool
	Password    PasswordResult
}

type AccountInput struct{ Username, Password, DisplayName, OriginalPassword string }

type PasswordResult struct {
	NotificationFailed, ProofRejected bool
	Data                              any
}

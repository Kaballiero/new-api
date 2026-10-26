package update_profile

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

// SelfUpdateStore keeps the previous writes of the account and its settings.
type SelfUpdateStore interface {
	SelfPreferences(id int) (SelfPreferencesAccount, error)
	SaveSelfPreferences(id int, settings domain.UserSetting) error
	UpdateSelfAccount(account SelfUpdateAccount) error
}

// SelfUpdateUser defers reading the ID until the settings branch is chosen.
type SelfUpdateUser interface{ UserID() int }

type Usecase struct {
	store     SelfUpdateStore
	input     AccountInputReader
	passwords PasswordChanger
}

// Preferences changes the settings; the sidebar takes priority over the
// language. It reports whether the request was a settings change.
func (update Usecase) Preferences(request map[string]any, user SelfUpdateUser) (bool, error) {
	sidebar, sidebarExists := request["sidebar_modules"]
	language, languageExists := request["language"]
	if !sidebarExists && !languageExists {
		return false, nil
	}
	account, err := update.store.SelfPreferences(user.UserID())
	if err != nil {
		return false, err
	}
	settings := account.Settings
	if sidebarExists {
		if value, ok := sidebar.(string); ok {
			settings.SidebarModules = value
		}
	} else if value, ok := language.(string); ok {
		settings.Language = value
	}
	if err := update.store.SaveSelfPreferences(account.ID, settings); err != nil {
		return false, domain.SettingsSaveFailed(err)
	}
	return true, nil
}

func NewUsecase(store SelfUpdateStore, input AccountInputReader, passwords PasswordChanger) Usecase {
	return Usecase{store: store, input: input, passwords: passwords}
}

// AccountInputReader returns domain.ErrProfileInvalid for an undecodable
// request and domain.ErrProfileInputFailed for failed field validation.
type AccountInputReader interface {
	ReadAccount() (AccountInput, error)
}
type PasswordChanger interface {
	Change(AccountInput, int) (PasswordResult, error)
}

func PasswordRequested(request map[string]any) bool {
	value, exists := request["password"]
	if !exists || value == nil {
		return false
	}
	password, isString := value.(string)
	return !isString || password != ""
}

// Execute gives settings priority and defers the password change until the
// input is validated. A failed password change is wrapped with
// domain.PasswordChangeFailed and still returns the password result; a failed
// settings load and a failed profile update are returned unclassified.
func (update Usecase) Execute(request map[string]any, user SelfUpdateUser) (SelfUpdateResult, error) {
	if !PasswordRequested(request) {
		changed, err := update.Preferences(request, user)
		if err != nil || changed {
			return SelfUpdateResult{Preferences: changed}, err
		}
	}
	account, err := update.input.ReadAccount()
	if err != nil {
		return SelfUpdateResult{}, err
	}
	id := user.UserID()
	if account.Password != "" {
		result, err := update.passwords.Change(account, id)
		if err != nil {
			return SelfUpdateResult{Password: result}, domain.PasswordChangeFailed(err)
		}
		return SelfUpdateResult{Password: result}, nil
	}
	return SelfUpdateResult{}, update.store.UpdateSelfAccount(SelfUpdateAccount{ID: id, Username: account.Username, Password: account.Password, DisplayName: account.DisplayName})
}

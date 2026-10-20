package update_profile

import "github.com/QuantumNous/new-api/relaykit/dto"

// SelfUpdateStore сохраняет прежние операции аккаунта и его настроек.
type SelfUpdateStore interface {
	SelfPreferences(id int) (SelfPreferencesAccount, error)
	SaveSelfPreferences(id int, settings dto.UserSetting) error
	UpdateSelfAccount(account SelfUpdateAccount) error
}

// SelfUpdateUser откладывает чтение ID до выбора ветки изменения настроек.
type SelfUpdateUser interface{ UserID() int }

type Usecase struct {
	Store     SelfUpdateStore
	input     AccountInputReader
	passwords PasswordChanger
}

type SelfUpdateIssue uint8

const (
	SelfUpdateValid SelfUpdateIssue = iota
	SelfUpdateLoadFailed
	SelfUpdatePreferencesFailed
)

// Preferences изменяет настройки, сохраняя приоритет боковой панели над языком.
func (update Usecase) Preferences(request map[string]any, user SelfUpdateUser) SelfUpdateResult {
	sidebar, sidebarExists := request["sidebar_modules"]
	language, languageExists := request["language"]
	if sidebarExists || languageExists {
		account, err := update.Store.SelfPreferences(user.UserID())
		if err != nil {
			return SelfUpdateResult{Issue: SelfUpdateLoadFailed, Err: err}
		}
		settings := account.Settings
		if sidebarExists {
			if value, ok := sidebar.(string); ok {
				settings.SidebarModules = value
			}
		} else if value, ok := language.(string); ok {
			settings.Language = value
		}
		if err := update.Store.SaveSelfPreferences(account.ID, settings); err != nil {
			return SelfUpdateResult{Issue: SelfUpdatePreferencesFailed, Err: err}
		}
		return SelfUpdateResult{Preferences: true}
	}

	return SelfUpdateResult{}
}

// UpdateAccount сохраняет обычное изменение после выбора сценария смены пароля.
func (update Usecase) UpdateAccount(account SelfUpdateAccount) error {
	return update.Store.UpdateSelfAccount(account)
}

func NewUsecase(store SelfUpdateStore, input AccountInputReader, passwords PasswordChanger) Usecase {
	return Usecase{Store: store, input: input, passwords: passwords}
}

type AccountInputReader interface {
	ReadAccount() (AccountInput, SelfUpdateIssue)
}
type PasswordChanger interface {
	Change(AccountInput, int) (PasswordResult, error)
}

const (
	InvalidParameters SelfUpdateIssue = iota + 3
	InvalidInput
	SecurityFailure
)

func PasswordRequested(request map[string]any) bool {
	value, exists := request["password"]
	if !exists || value == nil {
		return false
	}
	password, isString := value.(string)
	return !isString || password != ""
}

// Execute сохраняет приоритет настроек и откладывает смену пароля до проверки входа.
func (update Usecase) Execute(request map[string]any, user SelfUpdateUser) (SelfUpdateResult, PasswordResult) {
	if !PasswordRequested(request) {
		result := update.Preferences(request, user)
		if result.Issue != SelfUpdateValid || result.Preferences {
			return result, PasswordResult{}
		}
	}
	account, issue := update.input.ReadAccount()
	if issue != SelfUpdateValid {
		return SelfUpdateResult{Issue: issue}, PasswordResult{}
	}
	id := user.UserID()
	if account.Password != "" {
		result, err := update.passwords.Change(account, id)
		if err != nil {
			return SelfUpdateResult{Issue: SecurityFailure, Err: err}, result
		}
		return SelfUpdateResult{}, result
	}
	err := update.UpdateAccount(SelfUpdateAccount{ID: id, Username: account.Username, Password: account.Password, DisplayName: account.DisplayName})
	return SelfUpdateResult{Err: err}, PasswordResult{}
}

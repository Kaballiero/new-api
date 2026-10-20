package complete_oauth

import "errors"

type ProviderProfile struct{ ID, Username, DisplayName, Email, LegacyID string }
type AccountCreation interface {
	CreateUser(inviterID int) error
	BindProvider() error
}
type AccountLookup interface {
	Profile() ProviderProfile
	ProviderColumn() string
	FillByID(string) error
	MissingError() error
	TelegramUnboundError() error
	Taken(string) bool
	AccountID() int
	LogMigration(legacy, current string)
	MigrateGitHub(string) error
	LogMigrationFailure(error)
}
type AccountRegistration interface {
	RegistrationEnabled() bool
	PrepareDefaultUsername()
	UsernameExists(string) (bool, error)
	UsernameMaximum() int
	SetUsername(string)
	SetDisplayName(string)
	ProviderName() string
	SetNormalizedEmail(string)
	EnsureEmail() error
	EmailTakenError() error
	SetRoleAndStatus()
	FindInviter(string) (int, error)
	Transaction(func(AccountCreation) error) error
	Finalize(inviterID int)
}

func ResolveAccount(affiliateCode string, lookup AccountLookup, registration AccountRegistration) error {
	profile := lookup.Profile()
	if lookup.ProviderColumn() == "telegram_id" {
		err := lookup.FillByID(profile.ID)
		if errors.Is(err, lookup.MissingError()) {
			return lookup.TelegramUnboundError()
		}
		return err
	}
	if lookup.Taken(profile.ID) {
		if err := lookup.FillByID(profile.ID); err != nil {
			return err
		}
		if lookup.AccountID() == 0 {
			return &OAuthUserDeletedError{}
		}
		return nil
	}
	if profile.LegacyID != "" && lookup.Taken(profile.LegacyID) {
		if err := lookup.FillByID(profile.LegacyID); err != nil {
			return err
		}
		if lookup.AccountID() != 0 {
			lookup.LogMigration(profile.LegacyID, profile.ID)
			if err := lookup.MigrateGitHub(profile.ID); err != nil {
				lookup.LogMigrationFailure(err)
			}
			return nil
		}
	}
	if !registration.RegistrationEnabled() {
		return &OAuthRegistrationDisabledError{}
	}
	registration.PrepareDefaultUsername()
	if profile.Username != "" {
		if exists, err := registration.UsernameExists(profile.Username); err == nil && !exists {
			if len(profile.Username) <= registration.UsernameMaximum() {
				registration.SetUsername(profile.Username)
			}
		}
	}
	if profile.DisplayName != "" {
		registration.SetDisplayName(profile.DisplayName)
	} else if profile.Username != "" {
		registration.SetDisplayName(profile.Username)
	} else {
		registration.SetDisplayName(registration.ProviderName() + " User")
	}
	if profile.Email != "" {
		registration.SetNormalizedEmail(profile.Email)
		if err := registration.EnsureEmail(); err != nil {
			if errors.Is(err, registration.EmailTakenError()) {
				return &OAuthEmailAlreadyTakenError{}
			}
			return err
		}
	}
	registration.SetRoleAndStatus()
	inviter := 0
	if affiliateCode != "" {
		inviter, _ = registration.FindInviter(affiliateCode)
	}
	if err := registration.Transaction(func(tx AccountCreation) error {
		if err := tx.CreateUser(inviter); err != nil {
			return err
		}
		return tx.BindProvider()
	}); err != nil {
		return err
	}
	registration.Finalize(inviter)
	return nil
}

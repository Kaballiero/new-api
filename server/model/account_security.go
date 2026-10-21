package model

import (
	"errors"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	passwordadapter "github.com/QuantumNous/new-api/internal/adapter/identity/password"
	"github.com/QuantumNous/new-api/internal/identity/change_password"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"

	"gorm.io/gorm"
)

var (
	ErrCurrentPasswordInvalid = identitydomain.ErrCurrentPasswordInvalid
	ErrAccountPasswordState   = identitydomain.ErrPasswordState
	ErrLastLoginMethod        = identitydomain.ErrLastLoginMethod
	ErrAccountBindingChanged  = identitydomain.ErrBindingChanged
	ErrCannotDeleteRootUser   = errors.New("The root account cannot be deleted.")
)

// ChangeUserPassword rechecks the authorized session, password state and current
// password under the same user lock as the credential update. Only self-profile
// fields are accepted; a stale snapshot cannot restore roles or account status.
func ChangeUserPassword(identity AuthSessionIdentity, update *User, firstPassword bool) error {
	op := &passwordadapter.PasswordWriteOperation{DB: func() *gorm.DB { return DB }, Validate: ValidateAuthSessionWithTx, Increment: IncrementUserAuthVersionWithTx, Identity: identity, Update: (*record.User)(update)}
	return change_password.NewUsecase(op).Execute(firstPassword)
}

// AccountLoginMethods is a snapshot of administrator-enabled login mechanisms.
// Enrollment is checked inside the transaction, not trusted from this snapshot.
type AccountLoginMethods = identitydomain.LoginMethods

func UnbindUserOAuthForSession(identity AuthSessionIdentity, providerID int, enabled AccountLoginMethods) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var current User
		if err := tx.First(&current, identity.UserID).Error; err != nil {
			return err
		}
		var binding UserOAuthBinding
		if err := tx.Where("user_id = ? AND provider_id = ?", identity.UserID, providerID).First(&binding).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAccountBindingChanged
			}
			return err
		}
		hasLogin, err := identitydomain.HasAlternativeLogin(enabled, identitydomain.LoginCredentials{Password: current.Password, WeChatID: current.WeChatId, OAuthColumns: map[string]string{"github_id": current.GitHubId, "discord_id": current.DiscordId, "oidc_id": current.OidcId, "linux_do_id": current.LinuxDOId, "telegram_id": current.TelegramId}}, alternativeLoginReader{tx: tx, userID: identity.UserID, excludedProviderID: providerID})
		if err != nil {
			return err
		}
		if !hasLogin {
			return ErrLastLoginMethod
		}
		return tx.Delete(&binding).Error
	})
}

func UpdateUserBindColumnForSessionWithTx(tx *gorm.DB, identity AuthSessionIdentity, column, value string) error {
	if !userBindColumns[column] || value == "" {
		return ErrAccountBindingChanged
	}
	if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	// Preserve the existing ownership check within the transaction and update
	// only the chosen binding column, never a complete user snapshot.
	var count int64
	if err := tx.Model(&User{}).Where(column+" = ? AND id <> ?", value, identity.UserID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrExternalIdentityAlreadyClaimed
	}
	return tx.Model(&User{}).Where("id = ?", identity.UserID).Update(column, value).Error
}

func UpdateUserOAuthBindingForSessionWithTx(tx *gorm.DB, identity AuthSessionIdentity, providerID int, subject string) error {
	if providerID <= 0 || subject == "" || len(subject) > 256 {
		return ErrAccountBindingChanged
	}
	if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	var binding UserOAuthBinding
	err := tx.Where("user_id = ? AND provider_id = ?", identity.UserID, providerID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CreateUserOAuthBindingWithTx(tx, &UserOAuthBinding{UserId: identity.UserID, ProviderId: providerID, ProviderUserId: subject})
	}
	if err != nil {
		return err
	}
	// The existing unique provider/subject index rejects concurrent ownership.
	return tx.Model(&binding).Update("provider_user_id", subject).Error
}

// These SQL counts run under the caller's existing account/session locks.
type alternativeLoginReader struct {
	tx                         *gorm.DB
	userID, excludedProviderID int
}

func (reader alternativeLoginReader) Passkeys() (int64, error) {
	var count int64
	err := reader.tx.Model(&PasskeyCredential{}).Where("user_id = ?", reader.userID).Count(&count).Error
	return count, err
}
func (reader alternativeLoginReader) OtherOAuthBindings(providerIDs []int) (int64, error) {
	var count int64
	err := reader.tx.Model(&UserOAuthBinding{}).Where("user_id = ? AND provider_id <> ? AND provider_id IN ?", reader.userID, reader.excludedProviderID, providerIDs).Count(&count).Error
	return count, err
}

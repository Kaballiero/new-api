package identity

import (
	"errors"

	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"

	"gorm.io/gorm"
)

func (s Store) UnbindUserOAuthForSession(identity identitydomain.AuthIdentity, providerID int, enabled identitydomain.LoginMethods) error {
	return s.env.DB().Transaction(func(tx *gorm.DB) error {
		if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var current accountrecord.User
		if err := tx.First(&current, identity.UserID).Error; err != nil {
			return err
		}
		var binding identityrecord.UserOAuthBinding
		if err := tx.Where("user_id = ? AND provider_id = ?", identity.UserID, providerID).First(&binding).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrBindingChanged
			}
			return err
		}
		hasLogin, err := identitydomain.HasAlternativeLogin(enabled, identitydomain.LoginCredentials{Password: current.Password, WeChatID: current.WeChatId, OAuthColumns: map[string]string{"github_id": current.GitHubId, "discord_id": current.DiscordId, "oidc_id": current.OidcId, "linux_do_id": current.LinuxDOId, "telegram_id": current.TelegramId}}, alternativeLoginReader{tx: tx, userID: identity.UserID, excludedProviderID: providerID})
		if err != nil {
			return err
		}
		if !hasLogin {
			return identitydomain.ErrLastLoginMethod
		}
		return tx.Delete(&binding).Error
	})
}

func (s Store) UpdateUserBindColumnForSessionWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity, column, value string) error {
	if !s.env.AllowedBindColumn(column) || value == "" {
		return identitydomain.ErrBindingChanged
	}
	if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	// Preserve the existing ownership check within the transaction and update
	// only the chosen binding column, never a complete user snapshot.
	var count int64
	if err := tx.Model(&accountrecord.User{}).Where(column+" = ? AND id <> ?", value, identity.UserID).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return identitydomain.ErrIdentityClaimed
	}
	return tx.Model(&accountrecord.User{}).Where("id = ?", identity.UserID).Update(column, value).Error
}

func (s Store) UpdateUserOAuthBindingForSessionWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity, providerID int, subject string) error {
	if providerID <= 0 || subject == "" || len(subject) > 256 {
		return identitydomain.ErrBindingChanged
	}
	if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	var binding identityrecord.UserOAuthBinding
	err := tx.Where("user_id = ? AND provider_id = ?", identity.UserID, providerID).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.CreateUserOAuthBindingWithTx(tx, &identityrecord.UserOAuthBinding{UserId: identity.UserID, ProviderId: providerID, ProviderUserId: subject})
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
	err := reader.tx.Model(&identityrecord.PasskeyCredential{}).Where("user_id = ?", reader.userID).Count(&count).Error
	return count, err
}

func (reader alternativeLoginReader) OtherOAuthBindings(providerIDs []int) (int64, error) {
	var count int64
	err := reader.tx.Model(&identityrecord.UserOAuthBinding{}).Where("user_id = ? AND provider_id <> ? AND provider_id IN ?", reader.userID, reader.excludedProviderID, providerIDs).Count(&count).Error
	return count, err
}

// RemoveAccountAuthenticationFactors retains the caller's hard-deletion transaction.
func (s Store) RemoveAccountAuthenticationFactors(tx *gorm.DB, id int) error {
	for _, data := range []any{&identityrecord.TwoFABackupCode{}, &identityrecord.TwoFA{}, &identityrecord.UserSession{}, &identityrecord.AuthFlow{}, &identityrecord.PasskeyCredential{}} {
		if err := tx.Unscoped().Where("user_id = ?", id).Delete(data).Error; err != nil {
			return err
		}
	}
	return nil
}

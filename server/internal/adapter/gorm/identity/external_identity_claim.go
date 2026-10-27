package identity

import (
	"errors"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"gorm.io/gorm"
	"strings"
)

func (s Store) ReleaseExternalIdentityWithTx(tx *gorm.DB, provider string, userId int) error {
	provider = strings.TrimSpace(provider)
	if tx == nil || provider == "" || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("provider = ? AND user_id = ?", provider, userId).
		Delete(&identityrecord.ExternalIdentityClaim{}).Error
}

func (s Store) GetUserByTelegramID(telegramID string) (*accountrecord.User, error) {
	var user accountrecord.User
	err := s.env.DB().Where("telegram_id = ?", telegramID).First(&user).Error
	return &user, err
}

// BindTelegramForSessionWithTx preserves single ownership and the session that
// started the binding. The caller consumes its OAuth flow in this transaction.
func (s Store) BindTelegramForSessionWithTx(tx *gorm.DB, identity identitydomain.AuthIdentity, telegramID string) error {
	if err := s.ValidateAuthSessionWithTx(tx, identity); err != nil {
		return err
	}
	var user accountrecord.User
	if err := tx.Select("id", "telegram_id").First(&user, identity.UserID).Error; err != nil {
		return err
	}
	if user.TelegramId != "" {
		return identitydomain.ErrIdentityClaimed
	}
	if err := (identityrecord.ExternalIdentityClaim{}).Claim(tx, identityrecord.ExternalIdentityProviderTelegram, telegramID, user.Id); err != nil {
		return err
	}
	result := tx.Model(&accountrecord.User{}).Where("id = ? AND (telegram_id = ? OR telegram_id IS NULL)", user.Id, "").Update("telegram_id", telegramID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return identitydomain.ErrIdentityClaimed
	}
	return nil
}

func (s Store) ReleaseAllExternalIdentitiesWithTx(tx *gorm.DB, userId int) error {
	if tx == nil || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("user_id = ?", userId).Delete(&identityrecord.ExternalIdentityClaim{}).Error
}

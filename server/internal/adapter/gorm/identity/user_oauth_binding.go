package identity

import (
	"errors"
	"time"

	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"

	"gorm.io/gorm"
)

// GetUserOAuthBindingsByUserId returns all OAuth bindings for a user
func (s Store) GetUserOAuthBindingsByUserId(userId int) ([]*identityrecord.UserOAuthBinding, error) {
	var bindings []*identityrecord.UserOAuthBinding
	err := s.env.DB().Where("user_id = ?", userId).Find(&bindings).Error
	return bindings, err
}

// GetUserByOAuthBinding finds a user by provider ID and provider user ID
func (s Store) GetUserByOAuthBinding(providerId int, providerUserId string) (*accountrecord.User, error) {
	var binding identityrecord.UserOAuthBinding
	err := s.env.DB().Where("provider_id = ? AND provider_user_id = ?", providerId, providerUserId).First(&binding).Error
	if err != nil {
		return nil, err
	}

	var user accountrecord.User
	err = s.env.DB().First(&user, binding.UserId).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// IsProviderUserIdTaken checks if a provider user ID is already bound to any user
func (s Store) IsProviderUserIdTaken(providerId int, providerUserId string) bool {
	var count int64
	s.env.DB().Model(&identityrecord.UserOAuthBinding{}).Where("provider_id = ? AND provider_user_id = ?", providerId, providerUserId).Count(&count)
	return count > 0
}

// CreateUserOAuthBindingWithTx creates a new OAuth binding within a transaction
func (s Store) CreateUserOAuthBindingWithTx(tx *gorm.DB, binding *identityrecord.UserOAuthBinding) error {
	if binding.UserId == 0 {
		return errors.New("user ID is required")
	}
	if binding.ProviderId == 0 {
		return errors.New("provider ID is required")
	}
	if binding.ProviderUserId == "" {
		return errors.New("provider user ID is required")
	}

	// Check if this provider user ID is already taken (use tx to check within the same transaction)
	var count int64
	tx.Model(&identityrecord.UserOAuthBinding{}).Where("provider_id = ? AND provider_user_id = ?", binding.ProviderId, binding.ProviderUserId).Count(&count)
	if count > 0 {
		return errors.New("this OAuth account is already bound to another user")
	}

	binding.CreatedAt = time.Now()
	return tx.Create(binding).Error
}

// DeleteUserOAuthBinding deletes an OAuth binding
func (s Store) DeleteUserOAuthBinding(userId, providerId int) error {
	return s.env.DB().Where("user_id = ? AND provider_id = ?", userId, providerId).Delete(&identityrecord.UserOAuthBinding{}).Error
}

func (s Store) DeleteUserOAuthBindingsByUserId(tx *gorm.DB, userId int) error {
	return tx.Where("user_id = ?", userId).Delete(&identityrecord.UserOAuthBinding{}).Error
}

// GetBindingCountByProviderId returns the number of bindings for a provider
func (s Store) GetBindingCountByProviderId(providerId int) (int64, error) {
	var count int64
	err := s.env.DB().Model(&identityrecord.UserOAuthBinding{}).Where("provider_id = ?", providerId).Count(&count).Error
	return count, err
}

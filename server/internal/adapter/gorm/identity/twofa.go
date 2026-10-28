package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	locking "github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"

	"gorm.io/gorm"
)

// GetTwoFAByUserId loads a user factor.
func (s Store) GetTwoFAByUserId(userId int) (*identityrecord.TwoFA, error) {
	if userId == 0 {
		return nil, errors.New("用户ID不能为空")
	}

	var twoFA identityrecord.TwoFA
	err := s.env.DB().Where("user_id = ?", userId).First(&twoFA).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // A nil result denotes an unenrolled factor.
		}
		return nil, err
	}

	return &twoFA, nil
}

func (s Store) updateUsageState(t *identityrecord.TwoFA) error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}
	return s.env.DB().Model(&identityrecord.TwoFA{}).Where("id = ?", t.Id).Updates(map[string]any{
		"failed_attempts": t.FailedAttempts,
		"locked_until":    t.LockedUntil,
		"last_used_at":    t.LastUsedAt,
	}).Error
}

// IncrementFailedAttempts advances the failed-attempt counter.
func (s Store) IncrementFailedAttempts(t *identityrecord.TwoFA) error {
	if t.Id == 0 {
		return errors.New("2FA记录ID不能为空")
	}

	const maxUpdateRetries = 5
	for range maxUpdateRetries {
		var current identityrecord.TwoFA
		if err := s.env.DB().Select("id", "failed_attempts", "locked_until").First(&current, t.Id).Error; err != nil {
			return err
		}

		now := time.Now()
		if current.LockedUntil != nil && now.Before(*current.LockedUntil) {
			t.FailedAttempts = current.FailedAttempts
			t.LockedUntil = current.LockedUntil
			return nil
		}

		nextFailedAttempts := current.FailedAttempts + 1
		nextLockedUntil := current.LockedUntil
		if nextFailedAttempts >= common.MaxFailAttempts {
			lockUntil := now.Add(time.Duration(common.LockoutDuration) * time.Second)
			nextLockedUntil = &lockUntil
		}

		result := s.env.DB().Model(&identityrecord.TwoFA{}).
			Where("id = ? AND failed_attempts = ? AND (locked_until IS NULL OR locked_until <= ?)", current.Id, current.FailedAttempts, now).
			Updates(map[string]any{
				"failed_attempts": nextFailedAttempts,
				"locked_until":    nextLockedUntil,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}

		t.FailedAttempts = nextFailedAttempts
		t.LockedUntil = nextLockedUntil
		return nil
	}

	return errors.New("更新2FA失败次数冲突，请重试")
}

func (s Store) replaceBackupCodesWithTx(tx *gorm.DB, userId int, codes []string) error {
	if err := tx.Where("user_id = ?", userId).Delete(&identityrecord.TwoFABackupCode{}).Error; err != nil {
		return err
	}
	for _, code := range codes {
		hashedCode, err := common.HashBackupCode(code)
		if err != nil {
			return err
		}
		if err := tx.Create(&identityrecord.TwoFABackupCode{UserId: userId, CodeHash: hashedCode, IsUsed: false}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ReplaceBackupCodesWithAuthVersion atomically replaces the factor's recovery
// credentials and advances the user's authentication version.
func (s Store) ReplaceBackupCodesWithAuthVersion(userId int, codes []string) error {
	return s.replaceBackupCodesWithAuthVersion(userId, codes, nil)
}

func (s Store) ReplaceBackupCodesForSession(identity identitydomain.AuthIdentity, codes []string) error {
	return s.replaceBackupCodesWithAuthVersion(identity.UserID, codes, &identity)
}

func (s Store) replaceBackupCodesWithAuthVersion(userId int, codes []string, identity *identitydomain.AuthIdentity) error {
	if err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if identity != nil {
			if err := s.ValidateAuthSessionWithTx(tx, *identity); err != nil {
				return err
			}
		} else if err := locking.LockForUpdate(tx).Select("id").First(&accountrecord.User{}, userId).Error; err != nil {
			return err
		}
		var enabled identityrecord.TwoFA
		if err := locking.LockForUpdate(tx).Where("user_id = ? AND is_enabled = ?", userId, true).First(&enabled).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrTwoFANotEnabled
			}
			return err
		}
		if _, err := s.env.IncrementUserAuthVersionWithTx(tx, userId); err != nil {
			return err
		}
		return s.replaceBackupCodesWithTx(tx, userId, codes)
	}); err != nil {
		return err
	}
	return s.env.PublishUserAuthCache(userId)
}

// ValidateBackupCode verifies and consumes a recovery code.
func (s Store) ValidateBackupCode(userId int, code string) (bool, error) {
	if !common.ValidateBackupCode(code) {
		return false, errors.New("验证码或备用码不正确")
	}

	normalizedCode := common.NormalizeBackupCode(code)

	// Find unused recovery codes.
	var backupCodes []identityrecord.TwoFABackupCode
	if err := s.env.DB().Where("user_id = ? AND is_used = false", userId).Find(&backupCodes).Error; err != nil {
		return false, err
	}

	// Verify the recovery code.
	for _, bc := range backupCodes {
		if common.ValidatePasswordAndHash(normalizedCode, bc.CodeHash) {
			now := time.Now()
			result := s.env.DB().Model(&identityrecord.TwoFABackupCode{}).
				Where("id = ? AND is_used = ?", bc.Id, false).
				Updates(map[string]any{
					"is_used": true,
					"used_at": now,
				})
			if result.Error != nil {
				return false, result.Error
			}
			return result.RowsAffected == 1, nil
		}
	}

	return false, nil
}

// GetUnusedBackupCodeCount counts unused recovery codes.
func (s Store) GetUnusedBackupCodeCount(userId int) (int, error) {
	var count int64
	err := s.env.DB().Model(&identityrecord.TwoFABackupCode{}).Where("user_id = ? AND is_used = false", userId).Count(&count).Error
	return int(count), err
}

// DisableTwoFAWithAuthVersion atomically removes the factor and invalidates
// every access token issued against the previous security configuration.
func (s Store) DisableTwoFAWithAuthVersion(userId int) error {
	return s.disableTwoFAWithAuthVersion(userId, nil)
}

func (s Store) DisableTwoFAForSession(identity identitydomain.AuthIdentity) error {
	return s.disableTwoFAWithAuthVersion(identity.UserID, &identity)
}

func (s Store) disableTwoFAWithAuthVersion(userId int, identity *identitydomain.AuthIdentity) error {
	if err := s.env.DB().Transaction(func(tx *gorm.DB) error {
		if identity != nil {
			if err := s.ValidateAuthSessionWithTx(tx, *identity); err != nil {
				return err
			}
		} else if err := locking.LockForUpdate(tx).Select("id").First(&accountrecord.User{}, userId).Error; err != nil {
			return err
		}
		var twoFA identityrecord.TwoFA
		if err := locking.LockForUpdate(tx).Where("user_id = ? AND is_enabled = ?", userId, true).First(&twoFA).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identitydomain.ErrTwoFANotEnabled
			}
			return err
		}
		if _, err := s.env.IncrementUserAuthVersionWithTx(tx, userId); err != nil {
			return err
		}
		if err := tx.Unscoped().Where("user_id = ?", userId).Delete(&identityrecord.TwoFABackupCode{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&twoFA).Error
	}); err != nil {
		return err
	}
	return s.env.PublishUserAuthCache(userId)
}

// ValidateTOTPAndUpdateUsage verifies TOTP and records its use.
func (s Store) ValidateTOTPAndUpdateUsage(t *identityrecord.TwoFA, code string) (bool, error) {
	// Check the current lock deadline.
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// Verify the TOTP code.
	if !common.ValidateTOTPCode(t.Secret, code) {
		// Advance the failed-attempt counter.
		if err := s.IncrementFailedAttempts(t); err != nil {
			return false, err
		}
		return false, nil
	}

	// Reset failed attempts and record the successful-use timestamp.
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := s.updateUsageState(t); err != nil {
		return false, err
	}

	return true, nil
}

// ValidateBackupCodeAndUpdateUsage consumes a recovery code and records its use.
func (s Store) ValidateBackupCodeAndUpdateUsage(t *identityrecord.TwoFA, code string) (bool, error) {
	// Check the current lock deadline.
	if t.IsLocked() {
		return false, fmt.Errorf("账户已被锁定，请在%v后重试", t.LockedUntil.Format("2006-01-02 15:04:05"))
	}

	// Verify the recovery code.
	valid, err := s.ValidateBackupCode(t.UserId, code)
	if err != nil {
		return false, err
	}

	if !valid {
		// Advance the failed-attempt counter.
		if err := s.IncrementFailedAttempts(t); err != nil {
			return false, err
		}
		return false, nil
	}

	// Reset failed attempts and record the successful-use timestamp.
	now := time.Now()
	t.FailedAttempts = 0
	t.LockedUntil = nil
	t.LastUsedAt = &now

	if err := s.updateUsageState(t); err != nil {
		return false, err
	}

	return true, nil
}

// GetTwoFAStats returns administrator factor statistics.
func (s Store) GetTwoFAStats() (map[string]any, error) {
	var totalUsers, enabledUsers int64

	// Total user count.
	if err := s.env.DB().Model(&accountrecord.User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}

	// Enabled-factor count.
	if err := s.env.DB().Model(&identityrecord.TwoFA{}).Where("is_enabled = true").Count(&enabledUsers).Error; err != nil {
		return nil, err
	}

	enabledRate := float64(0)
	if totalUsers > 0 {
		enabledRate = float64(enabledUsers) / float64(totalUsers) * 100
	}

	return map[string]any{
		"total_users":   totalUsers,
		"enabled_users": enabledUsers,
		"enabled_rate":  fmt.Sprintf("%.1f%%", enabledRate),
	}, nil
}

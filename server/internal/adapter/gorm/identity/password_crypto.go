package identity

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const activeLoginEncryptionKeySlot = "active"

// InitPasswordEncryption loads the shared login-encryption key from its
// dedicated store. Concurrent replicas converge through the unique slot.
func (s Store) InitPasswordEncryption() error {
	var stored identityrecord.LoginEncryptionKey
	queryErr := s.env.DB().Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error
	if queryErr == nil {
		if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
			return fmt.Errorf("load persisted password encryption key: %w", err)
		}
		return nil
	}
	if !errors.Is(queryErr, gorm.ErrRecordNotFound) {
		return fmt.Errorf("read password encryption key: %w", queryErr)
	}

	privateKeyPEM, err := common.GeneratePasswordEncryptionPrivateKey()
	if err != nil {
		return err
	}
	candidate := identityrecord.LoginEncryptionKey{
		Slot:          activeLoginEncryptionKeySlot,
		PrivateKeyPEM: privateKeyPEM,
	}
	if err := s.env.DB().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slot"}},
		DoNothing: true,
	}).Create(&candidate).Error; err != nil {
		return fmt.Errorf("persist password encryption key: %w", err)
	}

	if err := s.env.DB().Where("slot = ?", activeLoginEncryptionKeySlot).First(&stored).Error; err != nil {
		return fmt.Errorf("reload password encryption key: %w", err)
	}
	if err := common.LoadPasswordEncryptionPrivateKey(stored.PrivateKeyPEM); err != nil {
		return fmt.Errorf("load persisted password encryption key: %w", err)
	}
	return nil
}

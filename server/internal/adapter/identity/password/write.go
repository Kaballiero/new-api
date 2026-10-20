package password

import (
	"github.com/QuantumNous/new-api/common"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/identity/change_password"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"gorm.io/gorm"
)

type PasswordWriteOperation struct {
	DB        func() *gorm.DB
	Validate  func(*gorm.DB, identitydomain.AuthIdentity) error
	Increment func(*gorm.DB, int) (int64, error)
	Identity  identitydomain.AuthIdentity
	Update    *record.User
	hash      string
	current   record.User
	tx        *gorm.DB
}

func (op *PasswordWriteOperation) HashRequested() error {
	var err error
	op.hash, err = common.HashAccountPassword(op.Update.Password)
	return err
}

func (op *PasswordWriteOperation) Transaction(mutate func(change_password.PasswordMutation) error) error {
	return op.DB().Transaction(func(tx *gorm.DB) error {
		if err := op.Validate(tx, op.Identity); err != nil {
			return err
		}
		op.tx = tx
		defer func() { op.tx = nil }()
		return mutate(op)
	})
}

func (op *PasswordWriteOperation) LoadCurrent() (bool, error) {
	if err := op.tx.First(&op.current, op.Identity.UserID).Error; err != nil {
		return false, err
	}
	return op.current.Password != "", nil
}

func (op *PasswordWriteOperation) OriginalMatches() bool {
	return common.ValidatePasswordAndHash(op.Update.OriginalPassword, op.current.Password)
}

func (op *PasswordWriteOperation) RequestedMatches() bool {
	return common.ValidatePasswordAndHash(op.Update.Password, op.current.Password)
}

func (op *PasswordWriteOperation) IncrementAuthVersion() error {
	_, err := op.Increment(op.tx, op.Identity.UserID)
	return err
}

func (op *PasswordWriteOperation) UpdateAndReload() error {
	changes := map[string]any{"password": op.hash}
	if op.Update.Username != "" {
		changes["username"] = op.Update.Username
	}
	if op.Update.DisplayName != "" {
		changes["display_name"] = op.Update.DisplayName
	}
	if err := op.tx.Model(&record.User{}).Where("id = ?", op.Identity.UserID).Updates(changes).Error; err != nil {
		return err
	}
	return op.tx.First(op.Update, op.Identity.UserID).Error
}

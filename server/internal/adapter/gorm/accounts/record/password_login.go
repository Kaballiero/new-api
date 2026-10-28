package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type PasswordLoginLookup struct {
	User                     *User
	DB                       func() *gorm.DB
	Empty, Invalid, Database error
}

func (op PasswordLoginLookup) Load(username string) (string, bool, error) {
	err := op.DB().Where("username = ? OR email = ?", username, username).First(op.User).Error
	return op.User.Password, op.User.Status == common.UserStatusEnabled, err
}
func (PasswordLoginLookup) Verify(password, hash string) bool {
	return common.ValidatePasswordAndHash(password, hash)
}
func (op PasswordLoginLookup) EmptyError() error   { return op.Empty }
func (PasswordLoginLookup) MissingError() error    { return gorm.ErrRecordNotFound }
func (op PasswordLoginLookup) InvalidError() error { return op.Invalid }
func (op PasswordLoginLookup) DatabaseFailure(err error) error {
	return fmt.Errorf("%w: %v", op.Database, err)
}

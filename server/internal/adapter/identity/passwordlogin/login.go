package passwordlogin

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
)

type Login struct {
	ValidateAndFill func(*accountrecord.User) error
}

func (Login) EncryptionEnabled() bool { return common.PasswordLoginEncryptionEnabled }
func (Login) Decrypt(encrypted, keyID string) (string, error) {
	return common.DecryptPassword(encrypted, keyID)
}
func (op Login) Authenticate(username, password string) (password_login.Account, error) {
	user := accountrecord.User{Username: username, Password: password}
	err := op.ValidateAndFill(&user)
	return password_login.Account{ID: user.Id, Status: user.Status, AuthVersion: user.AuthVersion}, err
}
func (Login) StorageFailed(err error) bool { return errors.Is(err, accountstore.ErrDatabase) }
func (Login) CredentialsEmpty(err error) bool {
	return errors.Is(err, accountdomain.ErrUserEmptyCredentials)
}
func (Login) LogDatabaseError(username string, err error) {
	common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
}

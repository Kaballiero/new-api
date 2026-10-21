package passwordlogin

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/model"
)

type Login struct{}

func (Login) EncryptionEnabled() bool { return common.PasswordLoginEncryptionEnabled }
func (Login) Decrypt(encrypted, keyID string) (string, error) {
	return common.DecryptPassword(encrypted, keyID)
}
func (Login) Authenticate(username, password string) (password_login.Account, error) {
	user := model.User{Username: username, Password: password}
	err := user.ValidateAndFill()
	return password_login.Account{ID: user.Id, Status: user.Status, AuthVersion: user.AuthVersion}, err
}
func (Login) StorageFailed(err error) bool    { return errors.Is(err, model.ErrDatabase) }
func (Login) CredentialsEmpty(err error) bool { return errors.Is(err, model.ErrUserEmptyCredentials) }
func (Login) LogDatabaseError(username string, err error) {
	common.SysLog(fmt.Sprintf("Login database error for user %s: %v", username, err))
}

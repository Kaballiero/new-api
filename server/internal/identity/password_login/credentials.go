package password_login

import (
	"errors"
	"strings"

	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type CredentialLookup interface {
	Load(username string) (hash string, enabled bool, err error)
	Verify(password, hash string) bool
	EmptyError() error
	MissingError() error
	InvalidError() error
	DatabaseFailure(error) error
}

func Authenticate(username, password string, op CredentialLookup) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return op.EmptyError()
	}
	hash, enabled, err := op.Load(username)
	if err != nil {
		if errors.Is(err, op.MissingError()) {
			return op.InvalidError()
		}
		return op.DatabaseFailure(err)
	}
	if !(identitydomain.PasswordCredential{Hash: hash, Enabled: enabled}).Accepts(password, op.Verify) {
		return op.InvalidError()
	}
	return nil
}

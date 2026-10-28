package password_login

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errStorage = errors.New("database error")
	errEmpty   = errors.New("empty credentials")
	errWrong   = errors.New("invalid credentials")
)

type fakeOperation struct {
	encryption bool
	decryptErr error
	authErr    error
	password   string
	logged     []string
}

func (f *fakeOperation) EncryptionEnabled() bool { return f.encryption }
func (f *fakeOperation) Decrypt(encrypted, _ string) (string, error) {
	return "plain-" + encrypted, f.decryptErr
}
func (f *fakeOperation) Authenticate(_, password string) (Account, error) {
	f.password = password
	if f.authErr != nil {
		return Account{}, f.authErr
	}
	return Account{ID: 3, Status: 1, AuthVersion: 9}, nil
}
func (f *fakeOperation) StorageFailed(err error) bool    { return errors.Is(err, errStorage) }
func (f *fakeOperation) CredentialsEmpty(err error) bool { return errors.Is(err, errEmpty) }
func (f *fakeOperation) LogDatabaseError(username string, err error) {
	f.logged = append(f.logged, username+": "+err.Error())
}

func TestExecuteRejections(t *testing.T) {
	cases := []struct {
		name   string
		op     fakeOperation
		input  LoginRequest
		want   error
		logged []string
	}{
		{"encrypted without key", fakeOperation{encryption: true}, LoginRequest{Username: "u", PasswordEncrypted: "x"}, domain.ErrInvalidInput, nil},
		{"decrypt failure", fakeOperation{encryption: true, decryptErr: errors.New("bad")}, LoginRequest{Username: "u", PasswordEncrypted: "x", EncryptionKeyID: "k"}, domain.ErrCredentialsInvalid, nil},
		{"empty username", fakeOperation{}, LoginRequest{Password: "p"}, domain.ErrInvalidInput, nil},
		{"storage", fakeOperation{authErr: errStorage}, LoginRequest{Username: "u", Password: "p"}, domain.ErrStorageUnavailable, []string{"u: database error"}},
		{"empty credentials", fakeOperation{authErr: errEmpty}, LoginRequest{Username: "u", Password: "p"}, domain.ErrInvalidInput, nil},
		{"wrong password", fakeOperation{authErr: errWrong}, LoginRequest{Username: "u", Password: "p"}, domain.ErrCredentialsInvalid, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			_, err := NewUsecase(&op).Execute(tc.input)
			assert.ErrorIs(t, err, tc.want)
			assert.Equal(t, tc.logged, op.logged)
		})
	}
}

func TestExecuteAuthenticatesDecryptedPassword(t *testing.T) {
	op := fakeOperation{encryption: true}
	account, err := NewUsecase(&op).Execute(LoginRequest{Username: "u", PasswordEncrypted: "secret", EncryptionKeyID: "k"})
	require.NoError(t, err)
	assert.Equal(t, Account{ID: 3, Status: 1, AuthVersion: 9}, account)
	assert.Equal(t, "plain-secret", op.password)
}

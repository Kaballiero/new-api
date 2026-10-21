package recover_password

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errUnresolved = errors.New("email not found")

type fakeOperation struct {
	codeValid bool
	resetErr  error
	deleted   []string
}

func (f *fakeOperation) NormalizeEmail(raw string) string   { return raw }
func (f *fakeOperation) VerifyCode(string, string) bool     { return f.codeValid }
func (f *fakeOperation) GeneratePassword() string           { return "generated" }
func (f *fakeOperation) ResetPassword(string, string) error { return f.resetErr }
func (f *fakeOperation) EmailUnresolved(err error) bool     { return errors.Is(err, errUnresolved) }
func (f *fakeOperation) DeleteCode(email string)            { f.deleted = append(f.deleted, email) }

func TestExecute(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name         string
		op           fakeOperation
		email, token string
		want         error
	}{
		{"missing token", fakeOperation{codeValid: true}, "a@b.c", "", domain.ErrInvalidInput},
		{"wrong code", fakeOperation{}, "a@b.c", "t", domain.ErrRecoveryLinkInvalid},
		{"unresolved email", fakeOperation{codeValid: true, resetErr: errUnresolved}, "a@b.c", "t", domain.ErrRecoveryLinkInvalid},
		{"storage", fakeOperation{codeValid: true, resetErr: storage}, "a@b.c", "t", storage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			_, err := NewUsecase(&op).Execute(tc.email, tc.token)
			assert.ErrorIs(t, err, tc.want)
			assert.Empty(t, op.deleted, "the code stays valid after a failed reset")
		})
	}
	op := fakeOperation{codeValid: true}
	password, err := NewUsecase(&op).Execute("a@b.c", "t")
	require.NoError(t, err)
	assert.Equal(t, "generated", password)
	assert.Equal(t, []string{"a@b.c"}, op.deleted)
}

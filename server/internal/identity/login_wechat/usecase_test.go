package login_wechat

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

type fakeOperation struct {
	exchangeErr  error
	taken        bool
	accountID    int
	registration bool
	enabled      bool
	inserted     bool
	lang         string
}

func (f *fakeOperation) ExchangeCode(string) error { return f.exchangeErr }
func (f *fakeOperation) InitializeAccount()        {}
func (f *fakeOperation) Taken() bool               { return f.taken }
func (f *fakeOperation) LoadAccount() error        { return nil }
func (f *fakeOperation) AccountID() int            { return f.accountID }
func (f *fakeOperation) RegistrationEnabled() bool { return f.registration }
func (f *fakeOperation) PrepareAccount()           {}
func (f *fakeOperation) InsertAccount(lang string) error {
	f.inserted, f.lang = true, lang
	return nil
}
func (f *fakeOperation) AccountEnabled() bool { return f.enabled }

func TestExecute(t *testing.T) {
	cases := []struct {
		name     string
		op       fakeOperation
		want     error
		inserted bool
	}{
		{"code", fakeOperation{exchangeErr: domain.ErrWeChatCodeInvalid}, domain.ErrWeChatCodeInvalid, false},
		{"deleted", fakeOperation{taken: true}, domain.ErrAccountDeleted, false},
		{"registration disabled", fakeOperation{}, domain.ErrRegistrationDisabled, false},
		{"banned", fakeOperation{taken: true, accountID: 3}, domain.ErrAccountBanned, false},
		{"registered", fakeOperation{registration: true, enabled: true}, nil, true},
		{"signed in", fakeOperation{taken: true, accountID: 3, enabled: true}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			err := NewUsecase().Execute(&op, Input{Code: "c", Lang: "ru"})
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, tc.inserted, op.inserted)
			if tc.inserted {
				assert.Equal(t, "ru", op.lang, "the registration texts use the request language")
			}
		})
	}
}

func TestExecuteKeepsExchangeErrors(t *testing.T) {
	cause := errors.New("wechat server unavailable")
	err := NewUsecase().Execute(&fakeOperation{exchangeErr: cause}, Input{Code: "c"})
	assert.Equal(t, cause, err, "the handler shows the provider text")
}

package bind_wechat

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOperation struct {
	exchangeErr, bindErr error
	taken, notifyFailed  bool
	succeeded, warning   bool
}

func (f *fakeOperation) ExchangeCode(string) error     { return f.exchangeErr }
func (f *fakeOperation) Taken() bool                   { return f.taken }
func (f *fakeOperation) BindAtomically() error         { return f.bindErr }
func (f *fakeOperation) MarkSucceeded()                { f.succeeded = true }
func (f *fakeOperation) LoadAccount() error            { return nil }
func (f *fakeOperation) NotifyAccount() bool           { return f.notifyFailed }
func (f *fakeOperation) SetNotificationWarning(w bool) { f.warning = w }

func TestExecute(t *testing.T) {
	cause := errors.New("provider says no")
	err := NewUsecase(&fakeOperation{exchangeErr: cause}).Execute(Input{Code: "c"})
	var provider *domain.ProviderError
	require.ErrorAs(t, err, &provider)
	assert.Equal(t, "provider says no", err.Error())

	op := &fakeOperation{taken: true}
	assert.ErrorIs(t, NewUsecase(op).Execute(Input{Code: "c"}), domain.ErrWeChatTaken)
	assert.False(t, op.succeeded)

	claimed := &fakeOperation{bindErr: domain.ErrIdentityClaimed}
	err = NewUsecase(claimed).Execute(Input{Code: "c"})
	assert.ErrorIs(t, err, domain.ErrIdentityClaimed)
	assert.False(t, errors.As(err, &provider))

	op = &fakeOperation{notifyFailed: true}
	require.NoError(t, NewUsecase(op).Execute(Input{Code: "c"}))
	assert.True(t, op.succeeded)
	assert.True(t, op.warning)
}

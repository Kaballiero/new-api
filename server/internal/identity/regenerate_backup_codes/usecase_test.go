package regenerate_backup_codes

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOperation struct{ generate, replace, advance error }

func (f fakeOperation) GenerateBackupCodes() error { return f.generate }
func (f fakeOperation) ReplaceBackupCodes() error  { return f.replace }
func (f fakeOperation) AdvanceSession() error      { return f.advance }

func TestExecute(t *testing.T) {
	cause := errors.New("cause")
	var logged *domain.LoggedError

	err := NewUsecase(fakeOperation{generate: cause}).Execute()
	assert.ErrorIs(t, err, domain.ErrBackupCodesGenerate)
	require.ErrorAs(t, err, &logged)
	assert.Equal(t, "cause", logged.Log)

	err = NewUsecase(fakeOperation{replace: cause}).Execute()
	assert.ErrorIs(t, err, domain.ErrBackupCodesSave)
	require.ErrorAs(t, err, &logged)
	assert.Equal(t, "cause", logged.Log)

	err = NewUsecase(fakeOperation{advance: domain.ErrLoginSessionRevoked}).Execute()
	assert.ErrorIs(t, err, domain.ErrLoginSessionRevoked)
	assert.False(t, errors.As(err, &logged), "session errors keep the session response")

	assert.NoError(t, NewUsecase(fakeOperation{}).Execute())
}

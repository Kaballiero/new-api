package consume_proof

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

type fakeOperation struct {
	consume  error
	captured bool
}

func (f *fakeOperation) BindOperation() error          { return nil }
func (f *fakeOperation) VerifyClaims() (string, error) { return "2fa", nil }
func (f *fakeOperation) ValidateSession() error        { return nil }
func (f *fakeOperation) RequireMethod(string) error    { return nil }
func (f *fakeOperation) ConsumeProof() error           { return f.consume }
func (f *fakeOperation) CaptureAuthorization()         { f.captured = true }

// Flow errors of the one-time proof become proof and token errors.
func TestExecuteMapsConsumption(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name    string
		consume error
		want    error
	}{
		{"replayed", domain.ErrAuthFlowConsumed, domain.ErrProofConsumed},
		{"expired", domain.ErrAuthFlowExpired, domain.ErrAuthTokenExpired},
		{"invalid", domain.ErrAuthFlowInvalid, domain.ErrAuthTokenInvalid},
		{"storage", storage, storage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &fakeOperation{consume: tc.consume}
			assert.ErrorIs(t, NewUsecase(op).Execute(), tc.want)
			assert.False(t, op.captured)
		})
	}
	op := &fakeOperation{}
	assert.NoError(t, NewUsecase(op).Execute())
	assert.True(t, op.captured)
}

package finish_twofa_setup

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
)

type fakeSetup struct{ load, authorize, enable error }

func (f fakeSetup) LoadSetupFlow() error         { return f.load }
func (f fakeSetup) DecodeAuthorization() error   { return nil }
func (f fakeSetup) ValidateAuthorization() error { return f.authorize }
func (f fakeSetup) EnableEnrollment() error      { return f.enable }

// A stale or replayed setup flow is reported as an invalid setup, not as a
// generic flow error.
func TestFinishTwoFASetup(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name string
		op   fakeSetup
		want error
	}{
		{"flow invalid", fakeSetup{load: domain.ErrAuthFlowInvalid}, domain.ErrTwoFASetupInvalid},
		{"flow expired", fakeSetup{load: fmt.Errorf("load: %w", domain.ErrAuthFlowExpired)}, domain.ErrTwoFASetupInvalid},
		{"flow consumed", fakeSetup{load: domain.ErrAuthFlowConsumed}, domain.ErrTwoFASetupInvalid},
		{"load failure", fakeSetup{load: storage}, storage},
		{"authorization flow", fakeSetup{authorize: domain.ErrAuthFlowInvalid}, domain.ErrTwoFASetupInvalid},
		{"authorization scope", fakeSetup{authorize: domain.ErrProofScope}, domain.ErrProofScope},
		{"code", fakeSetup{enable: domain.ErrTwoFACodeInvalid}, domain.ErrTwoFACodeInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, FinishTwoFASetup(tc.op), tc.want)
		})
	}
	assert.NoError(t, FinishTwoFASetup(fakeSetup{}))
}

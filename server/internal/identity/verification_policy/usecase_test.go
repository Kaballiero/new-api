package verification_policy

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

type method struct {
	Name      string
	Available bool
	Reason    string
}

type fakeOperation struct {
	passkey bool
	methods []method
}

func (f *fakeOperation) PasskeyEnabled() bool { return f.passkey }
func (f *fakeOperation) AddMethod(name string, available bool, reason string) {
	f.methods = append(f.methods, method{name, available, reason})
}

func TestExecute(t *testing.T) {
	const root = 100
	locked := domain.ErrVerificationLocked.Error()
	cases := []struct {
		name    string
		scope   string
		role    int
		state   FactorPolicyState
		passkey bool
		want    error
		methods []method
	}{
		{"unknown scope", "unknown", 1, FactorPolicyState{}, true, domain.ErrProofScope, nil},
		{"disable without 2fa", "2fa.disable", 1, FactorPolicyState{HasPassword: true}, true, domain.ErrTwoFANotEnabled, nil},
		{"setup with 2fa", "2fa.setup", 1, FactorPolicyState{HasTwoFA: true}, true, domain.ErrTwoFAAlreadyEnabled, nil},
		{"root deletion", "account.delete", root, FactorPolicyState{HasPassword: true}, true, domain.ErrVerificationForbidden, nil},
		{"set existing password", "account.password.set", 1, FactorPolicyState{HasPassword: true}, true, domain.ErrVerificationForbidden, nil},
		{"change missing password", "account.password.change", 1, FactorPolicyState{}, true, domain.ErrVerificationForbidden, nil},
		{"password fallback", "account.delete", 1, FactorPolicyState{HasPassword: true}, true, nil, []method{{"password", true, ""}}},
		{"oauth fallback", "account.binding.bind", 1, FactorPolicyState{}, true, nil, []method{{"oauth", true, ""}}},
		{"factors first", "passkey.delete", 1, FactorPolicyState{HasPassword: true, HasTwoFA: true, TwoFALocked: true, HasPasskey: true}, true, nil, []method{{"2fa", false, locked}, {"passkey", true, ""}}},
		{"passkey disabled", "passkey.register", 1, FactorPolicyState{HasTwoFA: true}, false, nil, []method{{"2fa", false, "Passkey authentication is disabled."}}},
		{"login without factors", "auth.login", 1, FactorPolicyState{HasPassword: true}, true, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := &fakeOperation{passkey: tc.passkey}
			err := NewUsecase(root, op).Execute(tc.scope, tc.role, tc.state)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, tc.methods, op.methods)
		})
	}
}

package verification_policy

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type FactorPolicyState struct{ HasPassword, HasTwoFA, TwoFALocked, HasPasskey bool }
type VerificationPolicyOperation interface {
	PasskeyEnabled() bool
	AddMethod(method string, available bool, reason string)
}

func (usecase Usecase) Execute(scope string, role int, state FactorPolicyState) error {
	var methods []string
	if state.HasTwoFA {
		methods = append(methods, "2fa")
	}
	if state.HasPasskey {
		methods = append(methods, "passkey")
	}
	switch scope {
	case "channel.key.read", "passkey.delete", "auth.login":
	case "2fa.disable", "2fa.backup_codes.regenerate":
		if !state.HasTwoFA {
			return usecase.errors.TwoFANotEnabled()
		}
	case "passkey.register", "2fa.setup", "access_token.generate", "access_token.revoke", "account.binding.bind", "account.binding.unbind", "account.password.set", "account.password.change", "account.delete":
		if scope == "account.delete" && role == usecase.rootRole {
			return usecase.errors.Forbidden()
		}
		if scope == "2fa.setup" && state.HasTwoFA {
			return usecase.errors.TwoFAAlreadyEnabled()
		}
		if (scope == "account.password.set" && state.HasPassword) || (scope == "account.password.change" && !state.HasPassword) {
			return usecase.errors.Forbidden()
		}
		if len(methods) == 0 {
			if state.HasPassword {
				methods = []string{"password"}
			} else {
				methods = []string{"oauth"}
			}
		}
	default:
		return usecase.errors.Scope()
	}
	for _, method := range methods {
		available, reason := true, ""
		if method == "2fa" && state.TwoFALocked {
			available, reason = false, usecase.errors.Locked().Error()
		}
		if !usecase.operation.PasskeyEnabled() && (method == "passkey" || scope == "passkey.register") {
			available, reason = false, "Passkey authentication is disabled."
		}
		usecase.operation.AddMethod(method, available, reason)
	}
	return nil
}

type Usecase struct {
	rootRole  int
	operation VerificationPolicyOperation
	errors    domain.VerificationErrors
}

func NewUsecase(rootRole int, operation VerificationPolicyOperation, errors domain.VerificationErrors) Usecase {
	return Usecase{rootRole: rootRole, operation: operation, errors: errors}
}

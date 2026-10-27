package verification_policy

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type FactorPolicyState struct{ HasPassword, HasTwoFA, TwoFALocked, HasPasskey bool }
type VerificationPolicyOperation interface {
	PasskeyEnabled() bool
	AddMethod(method string, available bool, reason string)
}

func (usecase Usecase) Execute(operation VerificationPolicyOperation, scope string, role int, state FactorPolicyState) error {
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
			return domain.ErrTwoFANotEnabled
		}
	case "passkey.register", "2fa.setup", "access_token.generate", "access_token.revoke", "account.binding.bind", "account.binding.unbind", "account.password.set", "account.password.change", "account.delete":
		if scope == "account.delete" && role == usecase.rootRole {
			return domain.ErrVerificationForbidden
		}
		if scope == "2fa.setup" && state.HasTwoFA {
			return domain.ErrTwoFAAlreadyEnabled
		}
		if (scope == "account.password.set" && state.HasPassword) || (scope == "account.password.change" && !state.HasPassword) {
			return domain.ErrVerificationForbidden
		}
		if len(methods) == 0 {
			if state.HasPassword {
				methods = []string{"password"}
			} else {
				methods = []string{"oauth"}
			}
		}
	default:
		return domain.ErrProofScope
	}
	for _, method := range methods {
		available, reason := true, ""
		if method == "2fa" && state.TwoFALocked {
			available, reason = false, domain.ErrVerificationLocked.Error()
		}
		if !operation.PasskeyEnabled() && (method == "passkey" || scope == "passkey.register") {
			available, reason = false, "Passkey authentication is disabled."
		}
		operation.AddMethod(method, available, reason)
	}
	return nil
}

type Usecase struct {
	rootRole int
}

func NewUsecase(rootRole int) Usecase {
	return Usecase{rootRole: rootRole}
}

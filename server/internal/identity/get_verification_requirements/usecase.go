package get_verification_requirements

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/require_verification_method"
)

type RequirementsOperation interface {
	LoadState() (status int, authVersion int64, role int, err error)
	Policy() error
	CaptureRequirements()
	require_verification_method.MethodOptions
	PasswordLoginEnabled() bool
	DisableMethod(index int, reason string)
	LoadOAuthAccount() error
	LoadOAuthProviders() (count int, err error)
	HasTelegram() bool
	TelegramConfigurationError() error
}

func (usecase Usecase) Execute(operation RequirementsOperation, scope string, expectedVersion int64) error {
	if scope == "auth.login" {
		return domain.ErrProofScope
	}
	status, version, role, err := operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || version != expectedVersion {
		return domain.ErrAuthTokenInvalid
	}
	if scope == "channel.key.read" && role != usecase.rootRole {
		return domain.ErrVerificationForbidden
	}
	if err := operation.Policy(); err != nil {
		return err
	}
	operation.CaptureRequirements()
	for i := range operation.MethodCount() {
		method, _ := operation.MethodOption(i)
		if method == "password" && !operation.PasswordLoginEnabled() {
			switch scope {
			case "account.binding.bind", "account.binding.unbind", "account.password.set", "account.password.change", "account.delete":
				operation.DisableMethod(i, "Password authentication is disabled.")
			}
		}
		if method != "oauth" {
			continue
		}
		if err := operation.LoadOAuthAccount(); err != nil {
			return err
		}
		count, err := operation.LoadOAuthProviders()
		if err != nil {
			return err
		}
		if count == 0 {
			operation.DisableMethod(i, "No linked OAuth provider is available.")
			if operation.HasTelegram() {
				if err := operation.TelegramConfigurationError(); err != nil {
					operation.DisableMethod(i, err.Error())
				}
			}
		}
	}
	return nil
}

type Usecase struct {
	enabledStatus int
	rootRole      int
}

func NewUsecase(enabledStatus int, rootRole int) Usecase {
	return Usecase{enabledStatus: enabledStatus, rootRole: rootRole}
}

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

func (usecase Usecase) Execute(scope string, expectedVersion int64) error {
	if scope == "auth.login" {
		return usecase.errors.Scope()
	}
	status, version, role, err := usecase.operation.LoadState()
	if err != nil {
		return err
	}
	if status != usecase.enabledStatus || version != expectedVersion {
		return usecase.errors.TokenInvalid()
	}
	if scope == "channel.key.read" && role != usecase.rootRole {
		return usecase.errors.Forbidden()
	}
	if err := usecase.operation.Policy(); err != nil {
		return err
	}
	usecase.operation.CaptureRequirements()
	for i := range usecase.operation.MethodCount() {
		method, _ := usecase.operation.MethodOption(i)
		if method == "password" && !usecase.operation.PasswordLoginEnabled() {
			switch scope {
			case "account.binding.bind", "account.binding.unbind", "account.password.set", "account.password.change", "account.delete":
				usecase.operation.DisableMethod(i, "Password authentication is disabled.")
			}
		}
		if method != "oauth" {
			continue
		}
		if err := usecase.operation.LoadOAuthAccount(); err != nil {
			return err
		}
		count, err := usecase.operation.LoadOAuthProviders()
		if err != nil {
			return err
		}
		if count == 0 {
			usecase.operation.DisableMethod(i, "No linked OAuth provider is available.")
			if usecase.operation.HasTelegram() {
				if err := usecase.operation.TelegramConfigurationError(); err != nil {
					usecase.operation.DisableMethod(i, err.Error())
				}
			}
		}
	}
	return nil
}

type Usecase struct {
	operation     RequirementsOperation
	enabledStatus int
	rootRole      int
	errors        domain.VerificationErrors
}

func NewUsecase(operation RequirementsOperation, enabledStatus int, rootRole int, errors domain.VerificationErrors) Usecase {
	return Usecase{operation: operation, enabledStatus: enabledStatus, rootRole: rootRole, errors: errors}
}

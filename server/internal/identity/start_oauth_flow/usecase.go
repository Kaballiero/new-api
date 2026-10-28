package start_oauth_flow

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	ProviderKnown() bool
	InitializePayload()
	NewTelegramFlow() error
	ReadIdentity() bool
	BeginBindingAudit()
	RequireBindingProof() (bool, error)
	HasTelegram() bool
	ValidateSession() error
	CaptureSession()
	StartVerification() error
	CreateFlow() error
	MarkSucceeded()
}

// Execute starts an OAuth flow. A rejected request is domain.ErrInvalidInput,
// binding or verification without a session is domain.ErrBindingSessionMissing,
// and domain.ErrStopped means the binding proof check already wrote the response.
func (usecase Usecase) Execute(input *Request) error {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Intent = strings.TrimSpace(input.Intent)
	input.Aff = strings.TrimSpace(input.Aff)
	if !usecase.operation.ProviderKnown() || (input.Intent != "login" && input.Intent != "bind" && input.Intent != "verify") || len(input.Aff) > 32 || (input.Intent != "login" && input.Aff != "") || (input.Intent != "verify" && (input.Scope != "" || len(input.Context) != 0)) {
		return domain.ErrInvalidInput
	}
	usecase.operation.InitializePayload()
	if input.Provider == "telegram" {
		if err := usecase.operation.NewTelegramFlow(); err != nil {
			return err
		}
	}
	if input.Intent == "bind" || input.Intent == "verify" {
		if !usecase.operation.ReadIdentity() {
			return domain.ErrBindingSessionMissing
		}
		if input.Intent == "bind" {
			usecase.operation.BeginBindingAudit()
			accepted, err := usecase.operation.RequireBindingProof()
			if err != nil {
				return err
			}
			if !accepted {
				return domain.ErrStopped
			}
			usecase.operation.CaptureSession()
		}
		if usecase.operation.HasTelegram() {
			if err := usecase.operation.ValidateSession(); err != nil {
				return err
			}
			usecase.operation.CaptureSession()
		}
		if input.Intent == "verify" {
			if err := usecase.operation.StartVerification(); err != nil {
				return err
			}
		}
	}
	if err := usecase.operation.CreateFlow(); err != nil {
		return err
	}
	usecase.operation.MarkSucceeded()
	return nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}

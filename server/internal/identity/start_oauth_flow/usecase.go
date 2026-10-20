package start_oauth_flow

import "strings"

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

func (usecase Usecase) Execute(input *Request) Result {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Intent = strings.TrimSpace(input.Intent)
	input.Aff = strings.TrimSpace(input.Aff)
	if !usecase.operation.ProviderKnown() || (input.Intent != "login" && input.Intent != "bind" && input.Intent != "verify") || len(input.Aff) > 32 || (input.Intent != "login" && input.Aff != "") || (input.Intent != "verify" && (input.Scope != "" || len(input.Context) != 0)) {
		return Result{Issue: "invalid"}
	}
	usecase.operation.InitializePayload()
	if input.Provider == "telegram" {
		if err := usecase.operation.NewTelegramFlow(); err != nil {
			return Result{Cause: err}
		}
	}
	if input.Intent == "bind" || input.Intent == "verify" {
		if !usecase.operation.ReadIdentity() {
			return Result{Issue: "session"}
		}
		if input.Intent == "bind" {
			usecase.operation.BeginBindingAudit()
			accepted, err := usecase.operation.RequireBindingProof()
			if err != nil {
				return Result{Cause: err}
			}
			if !accepted {
				return Result{Issue: "stopped"}
			}
			usecase.operation.CaptureSession()
		}
		if usecase.operation.HasTelegram() {
			if err := usecase.operation.ValidateSession(); err != nil {
				return Result{Cause: err}
			}
			usecase.operation.CaptureSession()
		}
		if input.Intent == "verify" {
			if err := usecase.operation.StartVerification(); err != nil {
				return Result{Cause: err}
			}
		}
	}
	if err := usecase.operation.CreateFlow(); err != nil {
		return Result{Cause: err}
	}
	usecase.operation.MarkSucceeded()
	return Result{}
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}

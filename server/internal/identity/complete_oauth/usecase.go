package complete_oauth

import "errors"

type CallbackOperation interface {
	InvalidFlow() error
	ProviderKnown() bool
	DecodeBindingPayload() error
	LoadPending() error
	PendingIntent() string
	ReadBoundIdentity() bool
	ValidateBindingAuthorization() error
	IsTelegram() bool
	TelegramConfiguration() error
	DecodeTelegram() error
	ReadTelegramIdentity() bool
	ValidateSession() error
	CaptureTelegramContext()
	ProviderEnabled() bool
	ProviderError() string
	ProviderErrorDescription(string) string
	ConsumeFlow() error
	ExchangeCode() error
	ReadProviderUser() error
	ConsumedIntent() string
	DecodePayload() error
	AffiliateCode() string
	AccountEnabled() bool
	VerifyBoundAccount() error
}
type BindingOperation interface {
	ReadBindingIdentity() bool
	InvalidIdentity() error
	DecodeBindingPayload() error
	ValidateBindingAuthorization() error
	ProviderUserTaken() bool
	LegacyUserTaken() bool
	CommitBinding() error
	LoadBoundAccount() error
	NotifyBoundAccount() bool
}

func CompleteBinding(op BindingOperation) Result {
	if !op.ReadBindingIdentity() {
		return Result{Cause: op.InvalidIdentity()}
	}
	if err := op.DecodeBindingPayload(); err != nil {
		return Result{Cause: err}
	}
	if err := op.ValidateBindingAuthorization(); err != nil {
		return Result{Cause: err}
	}
	if op.ProviderUserTaken() || op.LegacyUserTaken() {
		return Result{Issue: "already_bound"}
	}
	if err := op.CommitBinding(); err != nil {
		return Result{Cause: err}
	}
	if err := op.LoadBoundAccount(); err != nil {
		return Result{Cause: err, BindingSucceeded: true, NotificationWarning: true}
	}
	return Result{Issue: "bound", BindingSucceeded: true, NotificationWarning: op.NotifyBoundAccount()}
}
func (usecase Usecase) Execute() Result {
	if !usecase.operation.ProviderKnown() {
		return Result{Issue: "provider_unknown"}
	}
	if err := usecase.operation.LoadPending(); err != nil {
		return Result{Issue: "state"}
	}
	intent := usecase.operation.PendingIntent()
	if intent == "bind" || intent == "verify" {
		if !usecase.operation.ReadBoundIdentity() {
			return Result{Issue: "state"}
		}
		if intent == "bind" {
			if err := usecase.operation.DecodeBindingPayload(); err != nil {
				return Result{Cause: err}
			}
			if err := usecase.operation.ValidateBindingAuthorization(); err != nil {
				return Result{Cause: err}
			}
		}
	} else if intent != "login" {
		return Result{Issue: "invalid"}
	}
	if usecase.operation.IsTelegram() {
		if err := usecase.operation.TelegramConfiguration(); err != nil {
			return Result{Cause: err}
		}
		if err := usecase.operation.DecodeTelegram(); err != nil {
			return Result{Cause: err}
		}
		if intent != "login" {
			if !usecase.operation.ReadTelegramIdentity() {
				return Result{Cause: usecase.operation.InvalidFlow()}
			}
			if err := usecase.operation.ValidateSession(); err != nil {
				return Result{Cause: err}
			}
		}
		usecase.operation.CaptureTelegramContext()
	}
	if !usecase.operation.ProviderEnabled() {
		return Result{Issue: "provider_disabled"}
	}
	if code := usecase.operation.ProviderError(); code != "" {
		if err := usecase.operation.ConsumeFlow(); err != nil {
			return Result{Issue: "state"}
		}
		return Result{Issue: "provider_error", Message: usecase.operation.ProviderErrorDescription(code)}
	}
	if err := usecase.operation.ExchangeCode(); err != nil {
		if usecase.operation.IsTelegram() {
			return Result{Cause: err}
		}
		return Result{Issue: "external", Cause: err}
	}
	if err := usecase.operation.ReadProviderUser(); err != nil {
		if usecase.operation.IsTelegram() {
			return Result{Cause: err}
		}
		return Result{Issue: "external", Cause: err}
	}
	if intent == "bind" {
		return CompleteBinding(usecase.binding)
	}
	if err := usecase.operation.ConsumeFlow(); err != nil {
		return Result{Issue: "state"}
	}
	switch usecase.operation.ConsumedIntent() {
	case "login":
		if err := usecase.operation.DecodePayload(); err != nil {
			return Result{Cause: err}
		}
		if err := ResolveAccount(usecase.operation.AffiliateCode(), usecase.lookup, usecase.registration); err != nil {
			if errors.Is(err, usecase.registration.EmailTakenError()) {
				return Result{Issue: "email_taken"}
			}
			switch err.(type) {
			case *OAuthUserDeletedError:
				return Result{Issue: "deleted"}
			case *OAuthRegistrationDisabledError:
				return Result{Issue: "registration_disabled"}
			case *OAuthEmailAlreadyTakenError:
				return Result{Issue: "email_taken"}
			default:
				return Result{Cause: err}
			}
		}
		if !usecase.operation.AccountEnabled() {
			return Result{Issue: "banned"}
		}
		return Result{Issue: "login"}
	case "verify":
		if err := usecase.operation.DecodePayload(); err != nil {
			return Result{Cause: err}
		}
		if err := usecase.operation.VerifyBoundAccount(); err != nil {
			return Result{Cause: err}
		}
		return Result{Issue: "verified"}
	}
	return Result{Issue: "stopped"}
}

type Usecase struct {
	operation    CallbackOperation
	binding      BindingOperation
	lookup       AccountLookup
	registration AccountRegistration
}

func NewUsecase(operation CallbackOperation, binding BindingOperation, lookup AccountLookup, registration AccountRegistration) Usecase {
	return Usecase{operation: operation, binding: binding, lookup: lookup, registration: registration}
}

package complete_oauth

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type CallbackOperation interface {
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
	DecodeBindingPayload() error
	ValidateBindingAuthorization() error
	ProviderUserTaken() bool
	LegacyUserTaken() bool
	CommitBinding() error
	LoadBoundAccount() error
	NotifyBoundAccount() bool
}

// CompleteBinding binds the provider account to the session user. The result
// reports a committed binding even when loading the bound account fails.
func CompleteBinding(op BindingOperation) (Result, error) {
	if !op.ReadBindingIdentity() {
		return Result{}, domain.ErrAuthTokenInvalid
	}
	if err := op.DecodeBindingPayload(); err != nil {
		return Result{}, err
	}
	if err := op.ValidateBindingAuthorization(); err != nil {
		return Result{}, err
	}
	if op.ProviderUserTaken() || op.LegacyUserTaken() {
		return Result{}, domain.ErrAlreadyBound
	}
	if err := op.CommitBinding(); err != nil {
		return Result{}, err
	}
	if err := op.LoadBoundAccount(); err != nil {
		return Result{BindingSucceeded: true, NotificationWarning: true}, err
	}
	return Result{Outcome: OutcomeBound, BindingSucceeded: true, NotificationWarning: op.NotifyBoundAccount()}, nil
}

// Execute completes the OAuth callback. Provider exchange failures are
// *domain.ProviderError, a provider-reported error is
// *domain.ProviderMessageError; the result outcome is empty when the flow
// stops without a response.
func (usecase Usecase) Execute(operation CallbackOperation, binding BindingOperation, lookup AccountLookup, registration AccountRegistration) (Result, error) {
	if !operation.ProviderKnown() {
		return Result{}, domain.ErrProviderUnknown
	}
	if err := operation.LoadPending(); err != nil {
		return Result{}, domain.ErrOAuthStateInvalid
	}
	intent := operation.PendingIntent()
	if intent == "bind" || intent == "verify" {
		if !operation.ReadBoundIdentity() {
			return Result{}, domain.ErrOAuthStateInvalid
		}
		if intent == "bind" {
			if err := operation.DecodeBindingPayload(); err != nil {
				return Result{}, err
			}
			if err := operation.ValidateBindingAuthorization(); err != nil {
				return Result{}, err
			}
		}
	} else if intent != "login" {
		return Result{}, domain.ErrInvalidInput
	}
	if operation.IsTelegram() {
		if err := operation.TelegramConfiguration(); err != nil {
			return Result{}, err
		}
		if err := operation.DecodeTelegram(); err != nil {
			return Result{}, err
		}
		if intent != "login" {
			if !operation.ReadTelegramIdentity() {
				return Result{}, domain.ErrAuthFlowInvalid
			}
			if err := operation.ValidateSession(); err != nil {
				return Result{}, err
			}
		}
		operation.CaptureTelegramContext()
	}
	if !operation.ProviderEnabled() {
		return Result{}, domain.ErrProviderDisabled
	}
	if code := operation.ProviderError(); code != "" {
		if err := operation.ConsumeFlow(); err != nil {
			return Result{}, domain.ErrOAuthStateInvalid
		}
		return Result{}, &domain.ProviderMessageError{Message: operation.ProviderErrorDescription(code)}
	}
	if err := operation.ExchangeCode(); err != nil {
		if operation.IsTelegram() {
			return Result{}, err
		}
		return Result{}, &domain.ProviderError{Cause: err}
	}
	if err := operation.ReadProviderUser(); err != nil {
		if operation.IsTelegram() {
			return Result{}, err
		}
		return Result{}, &domain.ProviderError{Cause: err}
	}
	if intent == "bind" {
		return CompleteBinding(binding)
	}
	if err := operation.ConsumeFlow(); err != nil {
		return Result{}, domain.ErrOAuthStateInvalid
	}
	switch operation.ConsumedIntent() {
	case "login":
		if err := operation.DecodePayload(); err != nil {
			return Result{}, err
		}
		if err := ResolveAccount(operation.AffiliateCode(), lookup, registration); err != nil {
			if registration.EmailTaken(err) || errors.Is(err, domain.ErrEmailTaken) {
				return Result{}, domain.ErrEmailTaken
			}
			return Result{}, err
		}
		if !operation.AccountEnabled() {
			return Result{}, domain.ErrAccountBanned
		}
		return Result{Outcome: OutcomeLogin}, nil
	case "verify":
		if err := operation.DecodePayload(); err != nil {
			return Result{}, err
		}
		if err := operation.VerifyBoundAccount(); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeVerified}, nil
	}
	return Result{}, nil
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }

package service

import (
	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	"github.com/QuantumNous/new-api/internal/identity/confirm_email_binding"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/resend_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/start_email_binding"

	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"math/big"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var (
	ErrAccountEmailInvalid    = errors.New("Please enter a valid email address")
	ErrAccountEmailRestricted = errors.New("This email address is not allowed by the administrator's email policy.")
	ErrEmailBindingDelivery   = errors.New("Verification email could not be sent. Start email verification again.")
)

type EmailBindingData = identitydto.EmailBindingData

// ValidateAccountEmail is shared with registration mail delivery so binding
// cannot bypass the site's existing domain, alias and address-length rules.
func ValidateAccountEmail(email string) (string, error) {
	return identitydomain.ValidateEmail(email, identitydomain.EmailPolicy{Normalize: model.NormalizeEmail, Valid: func(value string) bool { return common.Validate.Var(value, "required,email,max=50") == nil }, DomainRestriction: func() bool { return common.EmailDomainRestrictionEnabled }, Whitelist: func() []string { return common.EmailDomainWhitelist }, AliasRestriction: func() bool { return common.EmailAliasRestrictionEnabled }, Invalid: func() error { return ErrAccountEmailInvalid }, Restricted: func() error { return ErrAccountEmailRestricted }})
}

func StartEmailBinding(identity AuthIdentity, authorization *model.AuthFlowAuthorization, email string) (*EmailBindingData, error) {
	op := &emailadapter.Operation{Dependencies: emailBindingDependencies(), Identity: identity, Authorization: authorization, Email: email}
	if err := start_email_binding.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Result, nil
}

func ResendAccountEmailBinding(identity AuthIdentity, token string) (*EmailBindingData, error) {
	op := &emailadapter.Operation{Dependencies: emailBindingDependencies(), Identity: identity, Token: token}
	if err := resend_email_binding.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Result, nil
}

func FinishEmailBinding(identity AuthIdentity, token, newCode, oldCode string) (*model.EmailBindingState, error) {
	op := &emailadapter.Operation{Dependencies: emailBindingDependencies(), Identity: identity, Token: token, NewCode: newCode, OldCode: oldCode}
	if err := confirm_email_binding.Finish(op); err != nil {
		return nil, err
	}
	return op.State, nil
}

type emailBindingCodes = emailadapter.Codes

func generateEmailBindingCodes(requireOld bool) (emailBindingCodes, error) {
	var codes emailBindingCodes
	count := 1
	if requireOld {
		count = 2
	}
	for index := 0; index < count; {
		number, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return codes, err
		}
		code := fmt.Sprintf("%06d", number.Int64())
		if code == codes.New {
			continue
		}
		hash, err := common.Password2Hash(code)
		if err != nil {
			return codes, err
		}
		if index == 0 {
			codes.New, codes.NewHash = code, hash
		} else {
			codes.Old, codes.OldHash = code, hash
		}
		index++
	}
	return codes, nil
}

func sendEmailBindingCodes(state model.EmailBindingState, codes emailBindingCodes) error {
	subject := common.SystemName + " — Confirm your email address"
	content := fmt.Sprintf("<p>Confirm linking this email address to your account.</p><p>Verification code: <strong>%s</strong></p><p>This code expires in 10 minutes. If you did not request this change, do not share this code.</p>", html.EscapeString(codes.New))
	if err := common.SendEmail(subject, state.Email, content); err != nil {
		return err
	}
	if codes.Old != "" {
		content = fmt.Sprintf("<p>A change to your account email address was requested. Confirm replacing your current address.</p><p>Verification code: <strong>%s</strong></p><p>This code expires in 10 minutes. If you did not request this change, do not share this code and contact your administrator.</p>", html.EscapeString(codes.Old))
		return common.SendEmail(subject, state.CurrentEmail, content)
	}
	return nil
}

func emailBindingData(token string, flow *model.AuthFlow, state *model.EmailBindingState) *EmailBindingData {
	return &EmailBindingData{
		FlowToken: token, Email: state.Email, CurrentEmail: common.MaskEmail(state.CurrentEmail),
		OldEmailRequired: state.OldCodeHash != "", ExpiresAt: flow.ExpiresAt.Unix(), ResendAt: state.ResendAt,
	}
}

func emailBindingDependencies() emailadapter.Dependencies {
	return emailadapter.Dependencies{ValidateEmailFunc: ValidateAccountEmail, ValidateAuthorizationFunc: ValidateFlowAuthorization, GenerateCodesFunc: generateEmailBindingCodes, DeliverFunc: sendEmailBindingCodes, DataFunc: emailBindingData, NotifyFunc: NotifyAccountSecurityChange, DeliveryErrorFunc: func() error { return ErrEmailBindingDelivery }}
}

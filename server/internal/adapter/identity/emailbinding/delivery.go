package emailbinding

import (
	"crypto/rand"
	"fmt"
	"html"
	"math/big"

	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

func ValidateAccountEmail(email string) (string, error) {
	return identitydomain.ValidateEmail(email, identitydomain.EmailPolicy{Normalize: accountstore.NormalizeEmail, Valid: func(value string) bool { return common.Validate.Var(value, "required,email,max=50") == nil }, DomainRestriction: func() bool { return common.EmailDomainRestrictionEnabled }, Whitelist: func() []string { return common.EmailDomainWhitelist }, AliasRestriction: func() bool { return common.EmailAliasRestrictionEnabled }})
}

func GenerateEmailBindingCodes(requireOld bool) (Codes, error) {
	var codes Codes
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

func SendEmailBindingCodes(state identitystore.EmailBindingState, codes Codes) error {
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

func BindingData(token string, flow *identityrecord.AuthFlow, state *identitystore.EmailBindingState) *identitydto.EmailBindingData {
	return &identitydto.EmailBindingData{
		FlowToken: token, Email: state.Email, CurrentEmail: common.MaskEmail(state.CurrentEmail),
		OldEmailRequired: state.OldCodeHash != "", ExpiresAt: flow.ExpiresAt.Unix(), ResendAt: state.ResendAt,
	}
}
func NotifyAccountSecurityChange(email, event string) error {
	if email == "" {
		return nil
	}
	subject := common.SystemName + " — Account security notification"
	content := fmt.Sprintf("<p>Your account security settings have changed: %s.</p><p>If you did not make this change, open your account security settings, revoke other login sessions, and contact your administrator.</p>", html.EscapeString(event))
	return common.SendEmail(subject, email, content)
}

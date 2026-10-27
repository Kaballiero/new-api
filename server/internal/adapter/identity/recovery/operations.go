package recovery

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
)

type EmailVerification struct{}

func (EmailVerification) ValidateEmail(raw string) (string, error) {
	return emailadapter.ValidateAccountEmail(raw)
}
func (EmailVerification) EmailTaken(email string) bool { return model.IsEmailAlreadyTaken(email) }
func (EmailVerification) GenerateCode(n int) string    { return common.GenerateVerificationCode(n) }
func (EmailVerification) RegisterCode(email, code string) {
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
}
func (EmailVerification) Deliver(email, code, lang string) error {
	subject, content := VerificationEmailMessage(lang, common.SystemName, code, common.VerificationValidMinutes)
	return common.SendEmail(subject, email, content)
}

// VerificationEmailMessage renders the subject and HTML body of the e-mail
// verification code in the language lang.
func VerificationEmailMessage(lang, systemName, code string, validMinutes int) (string, string) {
	data := map[string]any{"SystemName": systemName, "Code": code, "Minutes": validMinutes}
	return i18n.Translate(lang, i18n.MsgIdentityVerificationEmailSubject, data), i18n.Translate(lang, i18n.MsgIdentityVerificationEmailContent, data)
}

type PasswordResetEmail struct{ Context context.Context }

func (PasswordResetEmail) NormalizeEmail(raw string) string { return model.NormalizeEmail(raw) }
func (PasswordResetEmail) ValidEmail(email string) bool {
	return common.Validate.Var(email, "required,email") == nil
}
func (PasswordResetEmail) LookupAccount(email string) error {
	_, err := model.GetUniqueUserByEmail(email)
	return err
}
func (PasswordResetEmail) GenerateCode(n int) string { return common.GenerateVerificationCode(n) }
func (PasswordResetEmail) RegisterCode(email, code string) {
	common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
}
func (PasswordResetEmail) Deliver(email, code, lang string) error {
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.ServerAddress, email, code)
	subject, content := PasswordResetMessage(lang, common.SystemName, link, common.VerificationValidMinutes)
	return common.SendEmail(subject, email, content)
}

// PasswordResetMessage renders the subject and HTML body of the password reset
// e-mail with the reset link in the language lang.
func PasswordResetMessage(lang, systemName, link string, validMinutes int) (string, string) {
	data := map[string]any{"SystemName": systemName, "Link": link, "Minutes": validMinutes}
	return i18n.Translate(lang, i18n.MsgIdentityPasswordResetEmailSubject, data), i18n.Translate(lang, i18n.MsgIdentityPasswordResetEmailContent, data)
}
func (PasswordResetEmail) EmailNotFound(err error) bool {
	return errors.Is(err, model.ErrEmailNotFound)
}
func (op PasswordResetEmail) LogDeliveryFailure(email string, err error) {
	logger.LogError(op.Context, fmt.Sprintf("failed to send password reset email to %s: %s", email, err.Error()))
}
func (op PasswordResetEmail) LogLookupFailure(email string, err error) {
	logger.LogWarn(op.Context, fmt.Sprintf("skip password reset email for %s: %s", email, err.Error()))
}

type PasswordRecovery struct{}

func (PasswordRecovery) NormalizeEmail(raw string) string { return model.NormalizeEmail(raw) }
func (PasswordRecovery) VerifyCode(email, token string) bool {
	return common.VerifyCodeWithKey(email, token, common.PasswordResetPurpose)
}
func (PasswordRecovery) GeneratePassword() string { return common.GenerateVerificationCode(12) }
func (PasswordRecovery) ResetPassword(email, password string) error {
	return model.ResetUserPasswordByEmail(email, password)
}
func (PasswordRecovery) EmailUnresolved(err error) bool {
	return errors.Is(err, model.ErrEmailNotFound) || errors.Is(err, model.ErrEmailAmbiguous)
}
func (PasswordRecovery) DeleteCode(email string) {
	common.DeleteKey(email, common.PasswordResetPurpose)
}

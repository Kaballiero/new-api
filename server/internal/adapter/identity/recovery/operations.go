package recovery

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type EmailVerification struct{}

func (EmailVerification) ValidateEmail(raw string) (string, error) {
	return service.ValidateAccountEmail(raw)
}
func (EmailVerification) EmailTaken(email string) bool { return model.IsEmailAlreadyTaken(email) }
func (EmailVerification) GenerateCode(n int) string    { return common.GenerateVerificationCode(n) }
func (EmailVerification) RegisterCode(email, code string) {
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
}
func (EmailVerification) Deliver(email, code string) error {
	subject := fmt.Sprintf("%s邮箱验证邮件", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+"<p>您的验证码为: <strong>%s</strong></p>"+"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, code, common.VerificationValidMinutes)
	return common.SendEmail(subject, email, content)
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
func (PasswordResetEmail) Deliver(email, code string) error {
	link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.ServerAddress, email, code)
	subject := fmt.Sprintf("%s密码重置", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p>"+"<p>点击 <a href='%s'>此处</a> 进行密码重置。</p>"+"<p>如果链接无法点击，请尝试点击下面的链接或将其复制到浏览器中打开：<br> %s </p>"+"<p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, link, link, common.VerificationValidMinutes)
	return common.SendEmail(subject, email, content)
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

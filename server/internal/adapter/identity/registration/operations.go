package registration

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/register_password"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

type Operation struct{}

func (Operation) NormalizeEmail(email string) string         { return model.NormalizeEmail(email) }
func (Operation) Validate(user register_password.User) error { return common.Validate.Struct(&user) }
func (Operation) EmailVerificationEnabled() bool             { return common.EmailVerificationEnabled }
func (Operation) VerifyEmail(email, code string) bool {
	return common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose)
}
func (Operation) EnsureEmailAvailable(email string) error {
	return model.EnsureEmailAvailable(email, 0)
}
func (Operation) EmailTaken(err error) bool { return errors.Is(err, model.ErrEmailAlreadyTaken) }
func (Operation) CheckExists(username, email string) (bool, error) {
	return model.CheckUserExistOrDeleted(username, email)
}
func (Operation) Inviter(code string) (int, error) { return model.GetUserIdByAffCode(code) }
func (Operation) Insert(username, password, email string, inviter int, lang string) error {
	user := model.User{Username: username, Password: password, DisplayName: username, InviterId: inviter, Role: common.RoleCommonUser, Email: email}
	return user.Insert(inviter, lang)
}
func (Operation) FindInsertedID(username string) (int, error) {
	var user model.User
	err := model.DB.Where("username = ?", username).First(&user).Error
	return user.Id, err
}
func (Operation) DefaultTokenEnabled() bool         { return constant.GenerateDefaultToken }
func (Operation) GenerateTokenKey() (string, error) { return common.GenerateKey() }
func (Operation) InsertDefaultToken(id int, username, key, lang string) error {
	name := i18n.Translate(lang, i18n.MsgIdentityDefaultTokenName, map[string]any{"Username": username})
	token := model.Token{UserId: id, Name: name, Key: key, CreatedTime: common.GetTimestamp(), AccessedTime: common.GetTimestamp(), ExpiredTime: -1, RemainQuota: 500000, UnlimitedQuota: true, ModelLimitsEnabled: false}
	if setting.DefaultUseAutoGroup {
		token.Group = "auto"
	}
	return token.Insert()
}

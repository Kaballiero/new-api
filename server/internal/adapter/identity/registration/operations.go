package registration

import (
	"errors"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	tokenrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/identity/register_password"

	"github.com/QuantumNous/new-api/setting"
)

type Operation struct {
	FindInsertedUserID func(string) (int, error)
	Accounts           accountstore.Store
	InsertUser         func(*accountrecord.User, int, string) error
	InsertToken        func(*tokenrecord.Token) error
}

func (Operation) NormalizeEmail(email string) string         { return accountstore.NormalizeEmail(email) }
func (Operation) Validate(user register_password.User) error { return common.Validate.Struct(&user) }
func (Operation) EmailVerificationEnabled() bool             { return common.EmailVerificationEnabled }
func (Operation) VerifyEmail(email, code string) bool {
	return common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose)
}
func (op Operation) EnsureEmailAvailable(email string) error {
	return op.Accounts.EnsureEmailAvailable(email, 0)
}
func (Operation) EmailTaken(err error) bool {
	return errors.Is(err, accountdomain.ErrEmailAlreadyTaken)
}
func (op Operation) CheckExists(username, email string) (bool, error) {
	return op.Accounts.CheckUserExistOrDeleted(username, email)
}
func (op Operation) Inviter(code string) (int, error) { return op.Accounts.GetUserIdByAffCode(code) }
func (op Operation) Insert(username, password, email string, inviter int, lang string) error {
	user := accountrecord.User{Username: username, Password: password, DisplayName: username, InviterId: inviter, Role: common.RoleCommonUser, Email: email}
	return op.InsertUser(&user, inviter, lang)
}
func (op Operation) FindInsertedID(username string) (int, error) {
	return op.FindInsertedUserID(username)
}
func (Operation) DefaultTokenEnabled() bool         { return constant.GenerateDefaultToken }
func (Operation) GenerateTokenKey() (string, error) { return common.GenerateKey() }
func (op Operation) InsertDefaultToken(id int, username, key, lang string) error {
	name := i18n.Translate(lang, i18n.MsgIdentityDefaultTokenName, map[string]any{"Username": username})
	token := tokenrecord.Token{UserId: id, Name: name, Key: key, CreatedTime: common.GetTimestamp(), AccessedTime: common.GetTimestamp(), ExpiredTime: -1, RemainQuota: 500000, UnlimitedQuota: true, ModelLimitsEnabled: false}
	if setting.DefaultUseAutoGroup {
		token.Group = "auto"
	}
	return op.InsertToken(&token)
}

package oauthadapter

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/complete_oauth"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"gorm.io/gorm"
	"strconv"
)

type Account struct {
	Provider  oauth.Provider
	OAuthUser *oauth.OAuthUser
	User      *model.User
	// Lang is the language of the callback request for the persisted
	// registration log texts.
	Lang string
}

func (op *Account) Profile() complete_oauth.ProviderProfile {
	legacy, _ := op.OAuthUser.Extra["legacy_id"].(string)
	return complete_oauth.ProviderProfile{ID: op.OAuthUser.ProviderUserID, Username: op.OAuthUser.Username, DisplayName: op.OAuthUser.DisplayName, Email: op.OAuthUser.Email, LegacyID: legacy}
}
func (op *Account) ProviderColumn() string   { return op.Provider.ProviderUserIDColumn() }
func (op *Account) FillByID(id string) error { return op.Provider.FillUserByProviderID(op.User, id) }
func (op *Account) Missing(err error) bool   { return errors.Is(err, gorm.ErrRecordNotFound) }
func (op *Account) Taken(id string) bool     { return op.Provider.IsUserIDTaken(id) }
func (op *Account) AccountID() int           { return op.User.Id }
func (op *Account) LogMigration(legacy, current string) {
	common.SysLog(fmt.Sprintf("[OAuth] Migrating user %d from legacy_id=%s to new_id=%s", op.User.Id, legacy, current))
}
func (op *Account) MigrateGitHub(id string) error { return op.User.UpdateGitHubId(id) }
func (op *Account) LogMigrationFailure(err error) {
	common.SysError(fmt.Sprintf("[OAuth] Failed to migrate user %d: %s", op.User.Id, err.Error()))
}
func (op *Account) RegistrationEnabled() bool { return common.RegisterEnabled }
func (op *Account) PrepareDefaultUsername() {
	op.User.Username = op.Provider.GetProviderPrefix() + strconv.Itoa(model.GetMaxUserId()+1)
}
func (op *Account) UsernameExists(name string) (bool, error) {
	return model.CheckUserExistOrDeleted(name, "")
}
func (op *Account) UsernameMaximum() int            { return model.UserNameMaxLength }
func (op *Account) SetUsername(name string)         { op.User.Username = name }
func (op *Account) SetDisplayName(name string)      { op.User.DisplayName = name }
func (op *Account) ProviderName() string            { return op.Provider.GetName() }
func (op *Account) SetNormalizedEmail(email string) { op.User.Email = model.NormalizeEmail(email) }
func (op *Account) EnsureEmail() error              { return model.EnsureEmailAvailable(op.User.Email, 0) }
func (op *Account) EmailTaken(err error) bool       { return errors.Is(err, model.ErrEmailAlreadyTaken) }
func (op *Account) SetRoleAndStatus() {
	op.User.Role = common.RoleCommonUser
	op.User.Status = common.UserStatusEnabled
}
func (op *Account) FindInviter(code string) (int, error) { return model.GetUserIdByAffCode(code) }
func (op *Account) Transaction(run func(complete_oauth.AccountCreation) error) error {
	return model.DB.Transaction(func(tx *gorm.DB) error { return run(accountCreation{op: op, tx: tx}) })
}
func (op *Account) Finalize(inviter int) { op.User.FinalizeOAuthUserCreation(inviter, op.Lang) }

type accountCreation struct {
	op *Account
	tx *gorm.DB
}

func (tx accountCreation) CreateUser(inviter int) error {
	return tx.op.User.InsertWithTx(tx.tx, inviter)
}
func (tx accountCreation) BindProvider() error {
	op := tx.op
	if provider, ok := op.Provider.(*oauth.GenericOAuthProvider); ok {
		return model.CreateUserOAuthBindingWithTx(tx.tx, &model.UserOAuthBinding{UserId: op.User.Id, ProviderId: provider.GetProviderId(), ProviderUserId: op.OAuthUser.ProviderUserID})
	}
	op.Provider.SetProviderUserID(op.User, op.OAuthUser.ProviderUserID)
	return tx.tx.Model(op.User).Updates(map[string]any{"github_id": op.User.GitHubId, "discord_id": op.User.DiscordId, "oidc_id": op.User.OidcId, "linux_do_id": op.User.LinuxDOId, "wechat_id": op.User.WeChatId, "telegram_id": op.User.TelegramId}).Error
}

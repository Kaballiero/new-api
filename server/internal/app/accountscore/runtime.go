// Package accountscore assembles account mutations and their transaction capabilities.
package accountscore

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	clearbinding "github.com/QuantumNous/new-api/internal/accounts/clear_binding"
	"github.com/QuantumNous/new-api/internal/accounts/initialize_pat"
	"github.com/QuantumNous/new-api/internal/accounts/provision_account"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"sync"

	bindemail "github.com/QuantumNous/new-api/internal/accounts/bind_email"
	"github.com/QuantumNous/new-api/internal/accounts/delete_account"
	domain "github.com/QuantumNous/new-api/internal/accounts/domain"
	persist "github.com/QuantumNous/new-api/internal/accounts/persist_account"
	register "github.com/QuantumNous/new-api/internal/accounts/register_account"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	accountcore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	keyrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/apikeys/record"
	batchadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/batch"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/locking"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	identitydomain "github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	resetpassword "github.com/QuantumNous/new-api/internal/identity/reset_password"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/batchstate"
	"gorm.io/gorm"
)

// 根据用户角色生成默认的边栏配置
func GenerateDefaultSidebarConfigForRole(userRole int) string {
	configBytes, err := common.Marshal(domain.DashboardSidebarConfig(userRole, domain.AdminRoles{Admin: common.RoleAdminUser, Root: common.RoleRootUser}))
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}
	return string(configBytes)
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := record.User{Id: id}
	return HardDelete(&user)
}

func TransferAffQuotaToQuota(user *record.User, quota int) error {
	return transfer_affiliate_quota.TransferAffiliateQuota(quota, record.NewAffiliateTransfer((*record.User)(user), func() *gorm.DB { return model.DB }, locking.LockForUpdate), record.AffiliateQuotaDisplay{}, common.QuotaFromFloat)
}

// BindEmailToUser atomically checks email availability and assigns it to the
// user, serializing concurrent binds of the same email so two accounts cannot
// end up sharing one address. The email is normalized before check and store.
func BindEmailToUser(user *record.User, email string) error {
	return bindemail.NewUsecase(newEmailBindingOperation(user)).Execute(email)
}

func Delete(user *record.User) error {
	return delete(user, nil)
}

func DeleteUserForSession(identity identitydomain.AuthIdentity) error {
	user := record.User{Id: identity.UserID}
	return delete(&user, &identity)
}

func delete(user *record.User, identity *identitydomain.AuthIdentity) error {
	var source delete_account.DeletionSessionSource
	if identity != nil {
		source = func() domain.DeletionSession {
			return domain.DeletionSession{UserID: identity.UserID, SessionID: identity.SessionID, UserAuthVersion: identity.UserAuthVersion, SessionVersion: identity.SessionVersion}
		}
	}
	return delete_account.SoftDeleteAccount(newSoftDeletionOperation(user), source, common.RoleRootUser)
}

func HardDelete(user *record.User) error {
	return delete_account.HardDeleteAccount(newHardDeletionOperation(user))
}

// ValidateAndFill check password & user status
func ValidateAndFill(user *record.User) (err error) {
	return password_login.Authenticate(user.Username, user.Password, record.PasswordLoginLookup{User: (*record.User)(user), DB: func() *gorm.DB { return model.DB }, Empty: domain.ErrUserEmptyCredentials, Invalid: domain.ErrInvalidCredentials, Database: accountcore.ErrDatabase})
}

func ResetUserPasswordByEmail(email string, password string) error {
	return resetpassword.NewUsecase(newPasswordResetOperation()).Execute(email, password)
}

func RecordSystemLog(id int, message string) {
	usagestorage.Writer().RecordLog(id, model.LogTypeSystem, message)
}

// Insert registers the user; lang is the language of the acting request for
// the persisted registration and invitation log texts.
func Insert(user *record.User, inviterID int, lang string) error {
	operation := newRegistrationOperation(user, lang)
	return register.NewUsecase(operation, operation).Execute(inviterID, initialRegistrationAccount)
}

func initialRegistrationAccount() domain.InitialAccount {
	return domain.InitialAccount{Quota: common.QuotaForNewUser, AffiliateCode: common.GetRandomString(4)}
}

// FinishInsert completes a registration inserted with InsertWithTx; lang is the
// language of the acting request for the persisted log texts.
func FinishInsert(user *record.User, inviterID int, lang string) {
	operation := newRegistrationOperation(user, lang)
	register.NewUsecase(operation, operation).Finish(inviterID, false)
}

func InsertWithTx(user *record.User, tx *gorm.DB, inviterID int) error {
	// Inserting the defaults writes no log texts, so no language is needed.
	operation := newRegistrationOperation(user, "")
	return accountstorage.SQL().WithNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		return register.NewUsecase(operation, operation).InsertDefaults(record.NewRegistrationMutation((*record.User)(user), tx, accountstorage.SQL().EnsureEmailAvailableWithTx))
	})
}

func FinalizeOAuthUserCreation(user *record.User, inviterID int, lang string) {
	operation := newRegistrationOperation(user, lang)
	register.NewUsecase(operation, operation).Finish(inviterID, true)
}

func Edit(user *record.User, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, true)).Execute(password, true)
}

func UpdateWithTx(user *record.User, tx *gorm.DB, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).WithTransaction(record.NewPersistenceMutation((*record.User)(user), tx, password, false, accountstorage.Cache().IncrementUserAuthVersionWithTx, locking.LockForUpdate), password, false)
}

func EditWithTx(user *record.User, tx *gorm.DB, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).WithTransaction(record.NewPersistenceMutation((*record.User)(user), tx, password, true, accountstorage.Cache().IncrementUserAuthVersionWithTx, locking.LockForUpdate), password, true)
}

func newRegistrationOperation(user *record.User, lang string) record.RegistrationOperation {
	return record.NewRegistrationOperation((*record.User)(user), lang, record.RegistrationDependencies{
		DB: func() *gorm.DB { return model.DB }, EnsureEmail: accountstorage.SQL().EnsureEmailAvailableWithTx,
		WithEmailLock: accountstorage.SQL().WithNormalizedEmailLock, Sidebar: GenerateDefaultSidebarConfigForRole,
		SaveSidebar:     func(value *record.User) { _ = Update(value, false) },
		RecordSystemLog: RecordSystemLog, IncreaseUserQuota: accountstorage.SQL().IncreaseUserQuota, InviteUser: accountstorage.SQL().InviteUser,
	})
}

func newPersistenceOperation(user *record.User, edit bool) record.PersistenceOperation {
	return record.NewPersistenceOperation((*record.User)(user), edit, record.PersistenceDependencies{
		DB: func() *gorm.DB { return model.DB }, LockForUpdate: locking.LockForUpdate, AdvanceAuthVersion: accountstorage.Cache().IncrementUserAuthVersionWithTx,
		UpdateCache: func() error { return accountstorage.Cache().UpdateUserCache(*user) }, RevokeSessions: func(reason string) error {
			_, err := identitystorage.SQL().RevokeAllUserSessions(user.Id, reason)
			return err
		},
	})
}

func Update(user *record.User, password bool) error {
	return persist.NewUsecase(newPersistenceOperation(user, false)).Execute(password, false)
}

func ClearBinding(user *record.User, bindingType string) error {
	return clearbinding.NewBindingChange(record.NewBindingOperation((*record.User)(user), func() *gorm.DB { return model.DB }, identitystorage.SQL().ReleaseExternalIdentityWithTx, identityrecord.ExternalIdentityProviderTelegram, func() error { return accountstorage.Cache().UpdateUserCache(*user) })).Execute(bindingType)
}

func newPasswordResetOperation() *record.PasswordResetOperation {
	return record.NewPasswordResetOperation(record.ResetDependencies{DB: func() *gorm.DB { return model.DB }, FindUnique: func(email string) (*record.User, error) {
		user, err := accountstorage.SQL().GetUniqueUserByEmail(email)
		return (*record.User)(user), err
	}, Advance: accountstorage.Cache().IncrementUserAuthVersionWithTx, Publish: accountstorage.Cache().PublishUserAuthCache, Revoke: identitystorage.SQL().RevokeAllUserSessions})
}

func newEmailBindingOperation(user *record.User) record.EmailBindingOperation {
	return record.NewEmailBindingOperation((*record.User)(user), record.EmailDependencies{DB: func() *gorm.DB { return model.DB }, Normalize: accountcore.NormalizeEmail, WithEmailLock: accountstorage.SQL().WithNormalizedEmailLock, EnsureEmail: accountstorage.SQL().EnsureEmailAvailableWithTx, Save: func(tx *gorm.DB, value *record.User) error { return UpdateWithTx(value, tx, false) }, Cache: func() error { return accountstorage.Cache().UpdateUserCache(*user) }})
}

func newSoftDeletionOperation(user *record.User) record.SoftDeletionOperation {
	return record.NewSoftDeletionOperation((*record.User)(user), record.SoftDeletionDependencies{
		DB: func() *gorm.DB { return model.DB }, RootError: domain.ErrCannotDeleteRootUser, Publish: accountstorage.Cache().PublishCommittedUserAuthVersion, Revoke: identitystorage.SQL().RevokeAllUserSessions, Invalidate: accountstorage.Cache().InvalidateUserCache, Advance: accountstorage.Cache().IncrementUserAuthVersionWithTx,
		ValidateSession: func(tx *gorm.DB, s domain.DeletionSession) error {
			return identitystorage.SQL().ValidateAuthSessionWithTx(tx, identitydomain.AuthIdentity{UserID: s.UserID, SessionID: s.SessionID, UserAuthVersion: s.UserAuthVersion, SessionVersion: s.SessionVersion})
		},
	})
}

func newHardDeletionOperation(user *record.User) *record.HardDeletionOperation {
	var tokens []keyrecord.Token
	return record.NewHardDeletionOperation((*record.User)(user), record.HardDeletionDependencies{
		DB: func() *gorm.DB { return model.DB }, Lock: locking.LockForUpdate, Publish: accountstorage.Cache().PublishCommittedUserAuthVersion, Invalidate: accountstorage.Cache().InvalidateUserCache, Advance: accountstorage.Cache().IncrementUserAuthVersionWithTx,
		InvalidateCredentials: func() error { return apikeystorage.Quota().InvalidateTokensCache(tokens) },
		SnapshotCredentials: func(tx *gorm.DB) error {
			return tx.Unscoped().Select("id", model.KeyColumn).Where("user_id = ?", user.Id).Find(&tokens).Error
		},
		Conflict: batchstate.ErrConflict, FinancialDecision: batchadapter.AccountDeletionFinancialDecision, ReleaseIdentities: identitystorage.SQL().ReleaseAllExternalIdentitiesWithTx, RemoveFactors: identitystorage.SQL().RemoveAccountAuthenticationFactors,
		RemoveCredentials: func(tx *gorm.DB, id int) error {
			return tx.Unscoped().Where("user_id = ?", id).Delete(&keyrecord.Token{}).Error
		}, RemoveBindings: identitystorage.SQL().DeleteUserOAuthBindingsByUserId,
	})
}

func NewCheckinStore() record.CheckinStore {
	return record.NewCheckinStore(func() *gorm.DB { return model.DB }, accountstorage.SQL().IncreaseUserQuota, accountstorage.Cache().CacheIncrUserQuota)
}

func NewAccountGroupStore() record.GroupStore {
	return record.NewGroupStore(func() *gorm.DB { return model.DB }, accountstorage.Cache().RefreshUserGroupCache)
}

func NewQuotaAdjustmentStore() record.QuotaAdjustmentStore {
	return record.NewQuotaAdjustmentStore(func() *gorm.DB { return model.DB }, locking.LockForUpdate, accountstorage.Cache().CacheIncrUserQuota)
}

func NewProvision(sidebar func(int) string) provision_account.Usecase {
	operation := record.NewProvisionOperation(func() *gorm.DB { return model.DB }, accountstorage.SQL().EnsureUsernameAvailableWithTx, func(tx *gorm.DB, user *record.User) error { return InsertWithTx(user, tx, 0) })
	return provision_account.NewUsecase(operation, record.ProvisionCredentials{}, accountcore.IsUsernameUniqueViolation, ratio_setting.ContainsGroupRatio, sidebar, common.MaxQuota, common.RoleCommonUser, common.UserStatusEnabled)
}

var initializePAT = sync.OnceValue(func() initialize_pat.Usecase {
	return initialize_pat.NewUsecase(accountstorage.SQL(), func() (string, error) { return common.GenerateRandomKey(32) }, common.GetTimestamp, common.RoleCommonUser, common.UserStatusEnabled, common.UserStatusDisabled)
})

func InitializePAT() initialize_pat.Usecase { return initializePAT() }

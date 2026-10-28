package app

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/accounts/change_group"
	"github.com/QuantumNous/new-api/internal/accounts/check_in"
	"github.com/QuantumNous/new-api/internal/accounts/clear_binding"
	"github.com/QuantumNous/new-api/internal/accounts/create_account"
	"github.com/QuantumNous/new-api/internal/accounts/credit_getapi_quota"
	"github.com/QuantumNous/new-api/internal/accounts/delete_account"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/get_account"
	"github.com/QuantumNous/new-api/internal/accounts/get_account_models"
	"github.com/QuantumNous/new-api/internal/accounts/get_accounts_batch"
	"github.com/QuantumNous/new-api/internal/accounts/get_affiliate_code"
	"github.com/QuantumNous/new-api/internal/accounts/get_checkin_status"
	"github.com/QuantumNous/new-api/internal/accounts/get_groups"
	"github.com/QuantumNous/new-api/internal/accounts/get_profile"
	"github.com/QuantumNous/new-api/internal/accounts/initialize_pat"
	"github.com/QuantumNous/new-api/internal/accounts/list_accounts"
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/accounts/provision_account"
	"github.com/QuantumNous/new-api/internal/accounts/search_accounts"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/internal/accounts/transport"
	"github.com/QuantumNous/new-api/internal/accounts/update_account"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	accountsadapter "github.com/QuantumNous/new-api/internal/adapter/accounts"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	identityadapter "github.com/QuantumNous/new-api/internal/adapter/identity/accounts"
	accountssettings "github.com/QuantumNous/new-api/internal/adapter/settings/accounts"
	checkinsettings "github.com/QuantumNous/new-api/internal/adapter/settings/checkin"
	"github.com/QuantumNous/new-api/internal/app/accountscore"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	apikeystorage "github.com/QuantumNous/new-api/internal/app/apikeystorage"
	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	"github.com/QuantumNous/new-api/internal/app/identitycore"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/app/patstorage"
	usagestorage "github.com/QuantumNous/new-api/internal/app/usagestorage"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AccountsHTTP holds the explicitly assembled handlers of account scenarios.
type AccountsHTTP struct {
	CreditGetAPIQuota             *credit_getapi_quota.HTTP
	ClearBinding                  *clear_binding.HTTP
	CreateAccount                 *create_account.HTTP
	UpdateAccount                 *update_account.HTTP
	ManageAccount                 *manage_account.HTTP
	DeleteAccount                 *delete_account.HTTP
	GetAccount                    *get_account.HTTP
	ListAccounts                  *list_accounts.HTTP
	SearchAccounts                *search_accounts.HTTP
	GetAccountsBatch              *get_accounts_batch.HTTP
	ChangeGroup                   *change_group.HTTP
	GetGroups                     *get_groups.HTTP
	GetProfile                    *get_profile.HTTP
	UpdateProfile                 *update_profile.HTTP
	GetAccountModels              *get_account_models.HTTP
	GetAffiliateCode              *get_affiliate_code.HTTP
	TransferAffiliateQuota        *transfer_affiliate_quota.HTTP
	UpdateNotificationPreferences *update_notification_preferences.HTTP
	ProvisionAccount              *provision_account.HTTP
	InitializePAT                 *initialize_pat.HTTP
	CheckIn                       check_in.HTTP
	GetCheckinStatus              get_checkin_status.HTTP
}

// NewAccountsHTTP assembles the account handlers. Settings, the database and
// the request are read by the injected functions on each call, so the result
// can be built once.
func NewAccountsHTTP(effects AccountsEffects) AccountsHTTP {
	roles := domain.AdminRoles{Guest: common.RoleGuestUser, Common: common.RoleCommonUser, Admin: common.RoleAdminUser, Root: common.RoleRootUser, Enabled: common.UserStatusEnabled, Disabled: common.UserStatusDisabled}
	hooks := repository.AdminHooks{
		DB:  func() *gorm.DB { return model.DB },
		SQL: accountstorage.SQL(), InsertWithTx: accountscore.InsertWithTx, FinishInsert: accountscore.FinishInsert, EditWithTx: accountscore.EditWithTx, UpdateWithTx: accountscore.UpdateWithTx, Update: accountscore.Update, Delete: accountscore.Delete,
		SetPermissionsWithTx: func(tx *gorm.DB, id int, permissions map[string]map[string]bool) error {
			return authz.SetUserPermissionsInTx(tx, id, authz.PermissionsMap(permissions))
		},
		ClearAuthorizationWithTx: authz.ClearUserAuthorizationInTx,
		Reload:                   authz.ReloadPolicy,
		Revoke: func(id int, reason string) error {
			_, err := identitystorage.SQL().RevokeAllUserSessions(id, reason)
			return err
		},
		Publish:    accountstorage.Cache().PublishUserAuthCache,
		Invalidate: apikeystorage.Quota().InvalidateUserTokensCache,
	}
	quotaStore := accountscore.NewQuotaAdjustmentStore()
	adjustQuota := adjust_quota.NewHTTP(adjust_quota.NewUsecase(quotaStore, quotaStore, quotaStore, common.RoleRootUser, common.MaxWalletQuota, common.WalletQuotaFromDecimalStrict), effects.QuotaAudit)
	groups := accountssettings.UserGroups()
	profiles := repository.ProfileQueriesStore{SQL: accountstorage.SQL(), Update: accountscore.Update}
	return AccountsHTTP{
		ClearBinding: clear_binding.NewHTTP(func(c *gin.Context) clear_binding.AdminOperation {
			return &repository.ClearBindingOperation{SQL: accountstorage.SQL(), ClearBinding: accountscore.ClearBinding, Role: func() int { return c.GetInt("role") }, Audit: func(id int, username, bindingType string) {
				effects.ManageAudit(c, id, "user.binding_clear", map[string]any{"bindingType": bindingType, "username": username})
			}}
		}),
		CreateAccount: create_account.NewHTTP(func(c *gin.Context) create_account.Operation {
			return repository.NewAdminCreateOperation(hooks, func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.create", map[string]any{"username": target.Username, "role": target.Role})
			}, i18n.GetLangFromContext(c))
		}, roles),
		UpdateAccount: update_account.NewHTTP(func(c *gin.Context, user *domain.User) update_account.ResponseOperation {
			return repository.NewAdminEditOperation(user, hooks, func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.update", map[string]any{"username": target.Username, "id": target.ID})
			})
		}, roles),
		ManageAccount: manage_account.NewHTTP(func(c *gin.Context) manage_account.Operation {
			return repository.NewAdminManageOperation(hooks, func(target domain.AdminTarget, action string) {
				effects.ManageAudit(c, target.ID, "user.manage", map[string]any{"action": action, "username": target.Username, "id": target.ID})
			})
		}, roles, func(c *gin.Context, request manage_account.ManageRequest) {
			adjustQuota.Handle(c, adjust_quota.Request{Id: request.Id, Action: request.Action, Value: request.Value, Mode: request.Mode})
		}),
		GetAccount: get_account.NewHTTP(func() get_account.ResponseOperation { return &repository.AdminReadOperation{SQL: accountstorage.SQL()} }, func(id, role int) map[string]map[string]bool { return authz.Capabilities(id, role) }, common.RoleRootUser),
		DeleteAccount: delete_account.NewHTTP(func(c *gin.Context) delete_account.Operation {
			return &repository.AdminReadOperation{SQL: accountstorage.SQL(), Delete: accountscore.HardDeleteUserById, Audit: func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.delete", map[string]any{"username": target.Username, "id": target.ID})
			}}
		}),
		ListAccounts:     list_accounts.NewHTTP(repository.AdminQueriesStore{SQL: accountstorage.SQL()}),
		SearchAccounts:   search_accounts.NewHTTP(repository.AdminQueriesStore{SQL: accountstorage.SQL()}),
		GetAccountsBatch: get_accounts_batch.NewHTTP(repository.AdminQueriesStore{SQL: accountstorage.SQL()}, common.RoleRootUser),
		ChangeGroup:      change_group.NewHTTP(change_group.NewUsecase(accountscore.NewAccountGroupStore(), ratio_setting.ContainsGroupRatio, common.RoleRootUser)),
		GetGroups:        get_groups.NewHTTP(get_groups.NewUsecase(groups, accountGroups{})),
		GetProfile:       get_profile.NewHTTP(get_profile.NewUsecase(profiles, func(id, role int) any { return authz.Capabilities(id, role) }, roles, accountsadapter.SidebarModules)),
		GetAffiliateCode: get_affiliate_code.NewHTTP(get_affiliate_code.NewUsecase(profiles, common.GetRandomString)),
		GetAccountModels: get_account_models.NewHTTP(repository.AccountModelGroupReader{Cache: accountstorage.Cache()}, groups, channelstore.GetGroupsEnabledModels),
		UpdateProfile: update_profile.NewHTTP(repository.SelfUpdateStore{SQL: accountstorage.SQL(), Update: accountscore.Update}, func(c *gin.Context, succeeded *bool) update_profile.PasswordChanger {
			return identityadapter.NewPasswordChange(c, succeeded, accountstorage.SQL(), accountstorage.Cache(), identityadapter.PasswordCapabilities{ChangePassword: identitycore.ChangeUserPassword, AdvanceSession: identitycore.AdvanceCurrentSessionToUserVersion})
		}, func(c *gin.Context, succeeded, notificationFailed bool) {
			effects.SecurityAudit(c, c.GetInt("id"), "user.password_change", map[string]any{"success": succeeded, "notification_failed": notificationFailed})
		}, effects.SecurityError),
		TransferAffiliateQuota:        transfer_affiliate_quota.NewHTTP(repository.AffiliateAccountLoader{SQL: accountstorage.SQL(), Transfer: accountscore.TransferAffQuotaToQuota}, effects.PaymentCompliance),
		UpdateNotificationPreferences: update_notification_preferences.NewHTTP(update_notification_preferences.NewUsecase(repository.NotificationPreferencesStore{SQL: accountstorage.SQL()}, accountsadapter.NotificationTypes, common.RoleAdminUser)),
		ProvisionAccount:              provision_account.NewHTTP(accountscore.NewProvision(effects.Sidebar), effects.GetAPIError),
		InitializePAT:                 initialize_pat.NewHTTP(accountscore.InitializePAT(), effects.GetAPIError),
		CreditGetAPIQuota: credit_getapi_quota.NewHTTP(credit_getapi_quota.NewUsecase(accountstorage.SQL(), common.MaxWalletQuota,
			common.WalletQuotaFromDecimalStrict, common.GetTimestamp, accountstorage.Cache().CacheIncrUserQuota,
			func(id int, err error) {
				common.SysError(fmt.Sprintf("failed to sync quota credit for user %d: %s", id, err))
			}), effects.GetAPIError),
		CheckIn: check_in.NewHTTP(check_in.NewUsecase(accountscore.NewCheckinStore(), checkinsettings.Current), checkinsettings.Current, func(id, quota int, lang string) {
			usagestorage.Writer().RecordLog(id, usagerecord.LogTypeSystem, i18n.Translate(lang, i18n.MsgAccountCheckinLog, map[string]any{"Quota": logger.LogQuota(quota)}))
		}),
		GetCheckinStatus: get_checkin_status.NewHTTP(get_checkin_status.NewUsecase(accountscore.NewCheckinStore()), checkinsettings.Current),
	}
}

type accountGroups struct{}

func (accountGroups) UserGroup(id int, cache bool) (string, error) {
	return accountstorage.SQL().GetUserGroup(id, cache)
}

type AccountsEffects struct {
	ManageAudit       func(*gin.Context, int, string, map[string]any)
	SecurityAudit     func(*gin.Context, int, string, map[string]any)
	SecurityError     func(*gin.Context, error)
	PaymentCompliance func(*gin.Context) bool
	GetAPIError       func(*gin.Context, error)
	QuotaAudit        func(*gin.Context) adjust_quota.Audit
	Sidebar           func(int) string
}

func GetAPIErrors() transport.GetAPIErrors { return patstorage.GetAPIErrors() }

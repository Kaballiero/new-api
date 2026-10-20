package app

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/adjust_quota"
	"github.com/QuantumNous/new-api/internal/accounts/clear_binding"

	"github.com/QuantumNous/new-api/internal/accounts/change_group"
	"github.com/QuantumNous/new-api/internal/accounts/check_in"
	"github.com/QuantumNous/new-api/internal/accounts/create_account"
	"github.com/QuantumNous/new-api/internal/accounts/delete_account"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/internal/accounts/get_account"
	"github.com/QuantumNous/new-api/internal/accounts/get_account_models"
	"github.com/QuantumNous/new-api/internal/accounts/get_accounts_batch"
	"github.com/QuantumNous/new-api/internal/accounts/get_affiliate_code"
	"github.com/QuantumNous/new-api/internal/accounts/get_checkin_status"
	"github.com/QuantumNous/new-api/internal/accounts/get_groups"
	"github.com/QuantumNous/new-api/internal/accounts/get_profile"
	"github.com/QuantumNous/new-api/internal/accounts/list_accounts"
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/accounts/provision_account"
	"github.com/QuantumNous/new-api/internal/accounts/search_accounts"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/internal/accounts/update_account"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	"github.com/QuantumNous/new-api/internal/accounts/update_profile"
	accountshttp "github.com/QuantumNous/new-api/internal/adapter/accountshttp"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	identityadapter "github.com/QuantumNous/new-api/internal/adapter/identity/accounts"
	accountssettings "github.com/QuantumNous/new-api/internal/adapter/settings/accounts"
	checkinsettings "github.com/QuantumNous/new-api/internal/adapter/settings/checkin"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// AccountsHTTP содержит явно собранные обработчики сценариев аккаунтов.
type AccountsHTTP struct {
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
	CheckIn                       check_in.HTTP
	GetCheckinStatus              get_checkin_status.HTTP
}

func NewAccountsHTTP(effects AccountsEffects) AccountsHTTP {
	roles := domain.AdminRoles{Guest: common.RoleGuestUser, Common: common.RoleCommonUser, Admin: common.RoleAdminUser, Root: common.RoleRootUser, Enabled: common.UserStatusEnabled, Disabled: common.UserStatusDisabled}
	hooks := repository.AdminHooks{
		SetPermissionsWithTx: func(tx *gorm.DB, id int, permissions map[string]map[string]bool) error {
			return authz.SetUserPermissionsInTx(tx, id, authz.PermissionsMap(permissions))
		},
		ClearAuthorizationWithTx: authz.ClearUserAuthorizationInTx,
		Reload:                   authz.ReloadPolicy,
		Revoke:                   func(id int, reason string) error { _, err := model.RevokeAllUserSessions(id, reason); return err },
		Publish:                  model.PublishUserAuthCache,
		Invalidate:               model.InvalidateUserTokensCache,
	}
	quota := effects.Quota
	groups := accountssettings.UserGroups()
	profiles := repository.ProfileQueriesStore{}
	return AccountsHTTP{
		ClearBinding: clear_binding.NewHTTP(func(c *gin.Context) clear_binding.AdminOperation {
			return &repository.ClearBindingOperation{Role: func() int { return c.GetInt("role") }, Audit: func(id int, username, bindingType string) {
				effects.ManageAudit(c, id, "user.binding_clear", map[string]any{"bindingType": bindingType, "username": username})
			}}
		}),
		CreateAccount: create_account.NewHTTP(func(c *gin.Context) create_account.Operation {
			return repository.NewAdminCreateOperation(hooks, func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.create", map[string]any{"username": target.Username, "role": target.Role})
			})
		}, roles),
		UpdateAccount: update_account.NewHTTP(func(c *gin.Context, user *domain.User) update_account.ResponseOperation {
			return repository.NewAdminEditOperation(user, hooks, func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.update", map[string]any{"username": target.Username, "id": target.ID})
			})
		}, roles, func() error { return model.ErrUsernameAlreadyTaken }),
		ManageAccount: manage_account.NewHTTP(func(c *gin.Context) manage_account.Operation {
			return repository.NewAdminManageOperation(hooks, func(target domain.AdminTarget, action string) {
				effects.ManageAudit(c, target.ID, "user.manage", map[string]any{"action": action, "username": target.Username, "id": target.ID})
			})
		}, roles, func(c *gin.Context, request manage_account.ManageRequest) {
			quota(c, request)
		}),
		GetAccount: get_account.NewHTTP(func() get_account.ResponseOperation { return &repository.AdminReadOperation{} }, func(id, role int) map[string]map[string]bool { return authz.Capabilities(id, role) }, common.RoleRootUser),
		DeleteAccount: delete_account.NewHTTP(func(c *gin.Context) delete_account.Operation {
			return &repository.AdminReadOperation{Delete: model.HardDeleteUserById, Audit: func(target domain.AdminTarget) {
				effects.ManageAudit(c, target.ID, "user.delete", map[string]any{"username": target.Username, "id": target.ID})
			}}
		}),
		ListAccounts:     list_accounts.NewHTTP(repository.AdminQueriesStore{}),
		SearchAccounts:   search_accounts.NewHTTP(repository.AdminQueriesStore{}),
		GetAccountsBatch: get_accounts_batch.NewHTTP(repository.AdminQueriesStore{}, common.RoleRootUser),
		ChangeGroup:      change_group.NewHTTP(change_group.NewUsecase(model.NewAccountGroupStore(), ratio_setting.ContainsGroupRatio, common.RoleRootUser)),
		GetGroups:        get_groups.NewHTTP(get_groups.NewUsecase(groups, accountGroups{})),
		GetProfile:       get_profile.NewHTTP(get_profile.NewUsecase(profiles, func(id, role int) any { return authz.Capabilities(id, role) })),
		GetAffiliateCode: get_affiliate_code.NewHTTP(get_affiliate_code.NewUsecase(profiles, common.GetRandomString)),
		GetAccountModels: get_account_models.NewHTTP(repository.AccountModelGroupReader{}, groups, service.GetGroupsEnabledModels),
		UpdateProfile: update_profile.NewHTTP(repository.SelfUpdateStore{}, accountshttp.ProfileInput{}, identityadapter.NewPasswordChange, func(c *gin.Context, succeeded, notificationFailed bool) {
			effects.SecurityAudit(c, c.GetInt("id"), "user.password_change", map[string]any{"success": succeeded, "notification_failed": notificationFailed})
		}, effects.SecurityError),
		TransferAffiliateQuota:        transfer_affiliate_quota.NewHTTP(repository.AffiliateAccountLoader{}, effects.PaymentCompliance),
		UpdateNotificationPreferences: update_notification_preferences.NewHTTP(repository.NotificationPreferencesStore{}),
		ProvisionAccount:              provision_account.NewHTTP(provision_account.NewUsecase(model.NewProvisionOperation(), model.ProvisionCredentials{}, model.ProvisionErrors{}, ratio_setting.ContainsGroupRatio, effects.Sidebar, common.MaxQuota, common.RoleCommonUser, common.UserStatusEnabled), effects.GetAPIError, func() error { return model.ErrGetAPIInvalidRequest }),
		CheckIn:                       check_in.NewHTTP(check_in.NewUsecase(model.NewCheckinStore(), checkinsettings.Current), checkinsettings.Current, func(id int, message string) { model.RecordLog(id, model.LogTypeSystem, message) }),
		GetCheckinStatus:              get_checkin_status.NewHTTP(get_checkin_status.NewUsecase(model.NewCheckinStore()), checkinsettings.Current),
	}
}

type accountGroups struct{}

func (accountGroups) UserGroup(id int, cache bool) (string, error) {
	return model.GetUserGroup(id, cache)
}

type AccountsEffects struct {
	ManageAudit       func(*gin.Context, int, string, map[string]any)
	SecurityAudit     func(*gin.Context, int, string, map[string]any)
	SecurityError     func(*gin.Context, error)
	PaymentCompliance func(*gin.Context) bool
	GetAPIError       func(*gin.Context, error)
	Quota             func(*gin.Context, manage_account.ManageRequest)
	Sidebar           func(int) string
}

type recordErrors struct{}

func (recordErrors) IsRecordNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
func NewAccountQuotaHTTP(audit func(*gin.Context) adjust_quota.Audit) *adjust_quota.HTTP {
	store := model.NewQuotaAdjustmentStore()
	return adjust_quota.NewHTTP(adjust_quota.NewUsecase(store, store, store, store, common.RoleRootUser), audit, recordErrors{}, store)
}

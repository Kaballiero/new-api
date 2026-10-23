package controller

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/change_group"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	profiledto "github.com/QuantumNous/new-api/internal/accounts/dto"
	"github.com/QuantumNous/new-api/internal/accounts/get_accounts_batch"
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts"
	identityhttp "github.com/QuantumNous/new-api/internal/adapter/identity/http"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type LoginRequest = password_login.LoginRequest

func GetPasswordEncryptionKey(c *gin.Context) { identityHTTP().PasswordEncryptionKey.Handle(c) }

func Login(c *gin.Context) { identityHTTP().PasswordLogin.Handle(c) }

// loginMethodFromContext 根据请求路径推导登录方式，用于登录审计日志。
func loginMethodFromContext(c *gin.Context) string { return identityhttp.LoginMethodFromContext(c) }

// BRIDGE: successful login callbacks from identity workflows.
func recordLoginAudit(user *model.User, c *gin.Context) {
	auditRecording().Login(c, auditdomain.Actor{ID: user.Id, Username: user.Username, Role: user.Role}, loginMethodFromContext(c))
}

// setupLogin evaluates the shared login policy after primary authentication.
// Only a completed Passkey ceremony may go directly to session issuance.
func setupLogin(user *model.User, c *gin.Context) { identityHTTP().PrimaryLogin.Start(user, c) }

func setupLoginAtAuthVersion(user *model.User, version int64, c *gin.Context) {
	identityHTTP().PrimaryLogin.AtVersion(user, version, c)
}

func Register(c *gin.Context) { identityHTTP().RegisterPassword.Handle(c) }

func GetAllUsers(c *gin.Context) { accountsHTTP().ListAccounts.Handle(c) }

func SearchUsers(c *gin.Context) { accountsHTTP().SearchAccounts.Handle(c) }

type GetUsersBatchResponse = get_accounts_batch.GetUsersBatchResponse

func GetUsersBatch(c *gin.Context) { accountsHTTP().GetAccountsBatch.Handle(c) }

func GetUser(c *gin.Context) { accountsHTTP().GetAccount.Handle(c) }

type TransferAffQuotaRequest = transfer_affiliate_quota.TransferAffQuotaRequest

func TransferAffQuota(c *gin.Context) { accountsHTTP().TransferAffiliateQuota.Handle(c) }

func GetAffCode(c *gin.Context) { accountsHTTP().GetAffiliateCode.Handle(c) }

func GetSelf(c *gin.Context) { accountsHTTP().GetProfile.Handle(c) }

// buildSelfUserData is the single safe dashboard-user DTO used by GetSelf,
// login and refresh. It intentionally excludes password, management PAT and
// administrator-only remarks.
func buildSelfUserData(user *model.User) map[string]any {
	return profiledto.BuildSelfUserData(repository.ProfileData(user), func(id, role int) any { return authz.Capabilities(id, role) }, domain.AdminRoles{Admin: common.RoleAdminUser, Root: common.RoleRootUser})
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfig(userRole int) string {
	return model.GenerateDefaultSidebarConfigForRole(userRole)
}

func GetUserModels(c *gin.Context) { accountsHTTP().GetAccountModels.Handle(c) }

func UpdateUser(c *gin.Context) { accountsHTTP().UpdateAccount.Handle(c) }

func AdminClearUserBinding(c *gin.Context) { accountsHTTP().ClearBinding.Handle(c) }

func UpdateSelf(c *gin.Context) { accountsHTTP().UpdateProfile.Handle(c) }

func DeleteUser(c *gin.Context) { accountsHTTP().DeleteAccount.Handle(c) }

func DeleteSelf(c *gin.Context) { identityHTTP().DeleteAccount.Handle(c) }

func CreateUser(c *gin.Context) { accountsHTTP().CreateAccount.Handle(c) }

type ManageRequest = manage_account.ManageRequest

// ManageUserResponse is the result of a successful POST /api/user/manage call.
// Action is the executed action ("disable"|"enable"|"delete"|"promote"|"demote"|"add_quota").
// Role and Status reflect the user's state after the action; for quota actions
// Quota carries the new quota value. Some actions leave fields zero — clients
// should switch on Action to know which fields are meaningful.
type ManageUserResponse = manage_account.ManageUserResponse

// ManageUser Only admin user can do this
func ManageUser(c *gin.Context) { accountsHTTP().ManageAccount.Handle(c) }

type topUpRequest struct {
	Key string `json:"key"`
}

var topUpLocks sync.Map

var topUpCreateLock sync.Mutex

type topUpTryLock struct {
	ch chan struct{}
}

func newTopUpTryLock() *topUpTryLock {
	return &topUpTryLock{ch: make(chan struct{}, 1)}
}

func (l *topUpTryLock) TryLock() bool {
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *topUpTryLock) Unlock() {
	select {
	case <-l.ch:
	default:
	}
}

func getTopUpLock(userID int) *topUpTryLock {
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	topUpCreateLock.Lock()
	defer topUpCreateLock.Unlock()
	if v, ok := topUpLocks.Load(userID); ok {
		return v.(*topUpTryLock)
	}
	l := newTopUpTryLock()
	topUpLocks.Store(userID, l)
	return l
}

func TopUp(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	id := c.GetInt("id")
	lock := getTopUpLock(id)
	if !lock.TryLock() {
		common.ApiErrorI18n(c, i18n.MsgUserTopUpProcessing)
		return
	}
	defer lock.Unlock()
	req := topUpRequest{}
	err := c.ShouldBindJSON(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	quota, err := model.Redeem(req.Key, id)
	if err != nil {
		// 不向用户暴露兑换失败的细分原因，避免攻击者根据错误类型判断兑换码状态。
		common.ApiErrorI18n(c, i18n.MsgRedeemFailed)
		logger.LogError(c, fmt.Sprintf("failed to redeem key %s for user %d: %s", req.Key, id, err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    quota,
	})
}

type UpdateUserSettingRequest = update_notification_preferences.UpdateUserSettingRequest

func UpdateUserSetting(c *gin.Context) { accountsHTTP().UpdateNotificationPreferences.Handle(c) }

// BulkUpdateUserGroupRequest is the request body for POST /api/user/group/batch.
type BulkUpdateUserGroupRequest = change_group.BulkUpdateUserGroupRequest

// BulkUpdateUserGroupResponse summarizes the outcome of a bulk group update.
type BulkUpdateUserGroupResponse = change_group.BulkUpdateUserGroupResponse

// BulkUpdateUserGroup handles POST /api/user/group/batch — bulk update the `group` field
// of multiple users at once. Admin-only. Returns:
//   - 400 on empty input, oversize batch, or unknown target group
//   - 403 if every requested id is forbidden by the caller's role
//   - 404 if every requested id does not exist
//   - 200 with BulkUpdateUserGroupResponse on success (including partial: skipped/not_found are reported in the body)
func BulkUpdateUserGroup(c *gin.Context) { accountsHTTP().ChangeGroup.Handle(c) }

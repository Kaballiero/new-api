package controller

import (
	"github.com/QuantumNous/new-api/internal/accounts/change_group"
	"github.com/QuantumNous/new-api/internal/accounts/get_accounts_batch"
	"github.com/QuantumNous/new-api/internal/accounts/manage_account"
	"github.com/QuantumNous/new-api/internal/accounts/transfer_affiliate_quota"
	"github.com/QuantumNous/new-api/internal/accounts/update_notification_preferences"
	httpapi "github.com/QuantumNous/new-api/internal/app/httpapi"
	"github.com/QuantumNous/new-api/internal/identity/password_login"
	"github.com/gin-gonic/gin"
)

type LoginRequest = password_login.LoginRequest

func GetPasswordEncryptionKey(c *gin.Context) { httpapi.Identity().PasswordEncryptionKey.Handle(c) }

func Login(c *gin.Context) { httpapi.Identity().PasswordLogin.Handle(c) }

func Register(c *gin.Context) { httpapi.Identity().RegisterPassword.Handle(c) }

func GetAllUsers(c *gin.Context) { httpapi.Accounts().ListAccounts.Handle(c) }

func SearchUsers(c *gin.Context) { httpapi.Accounts().SearchAccounts.Handle(c) }

type GetUsersBatchResponse = get_accounts_batch.GetUsersBatchResponse

func GetUsersBatch(c *gin.Context) { httpapi.Accounts().GetAccountsBatch.Handle(c) }

func GetUser(c *gin.Context) { httpapi.Accounts().GetAccount.Handle(c) }

type TransferAffQuotaRequest = transfer_affiliate_quota.TransferAffQuotaRequest

func TransferAffQuota(c *gin.Context) { httpapi.Accounts().TransferAffiliateQuota.Handle(c) }

func GetAffCode(c *gin.Context) { httpapi.Accounts().GetAffiliateCode.Handle(c) }

func GetSelf(c *gin.Context) { httpapi.Accounts().GetProfile.Handle(c) }

func GetUserModels(c *gin.Context) { httpapi.Accounts().GetAccountModels.Handle(c) }

func UpdateUser(c *gin.Context) { httpapi.Accounts().UpdateAccount.Handle(c) }

func AdminClearUserBinding(c *gin.Context) { httpapi.Accounts().ClearBinding.Handle(c) }

func UpdateSelf(c *gin.Context) { httpapi.Accounts().UpdateProfile.Handle(c) }

func DeleteUser(c *gin.Context) { httpapi.Accounts().DeleteAccount.Handle(c) }

func DeleteSelf(c *gin.Context) { httpapi.Identity().DeleteAccount.Handle(c) }

func CreateUser(c *gin.Context) { httpapi.Accounts().CreateAccount.Handle(c) }

type ManageRequest = manage_account.ManageRequest

// ManageUserResponse is the result of a successful POST /api/user/manage call.
// Action is the executed action ("disable"|"enable"|"delete"|"promote"|"demote"|"add_quota").
// Role and Status reflect the user's state after the action; for quota actions
// Quota carries the new quota value. Some actions leave fields zero — clients
// should switch on Action to know which fields are meaningful.
type ManageUserResponse = manage_account.ManageUserResponse

func ManageUser(c *gin.Context) { httpapi.Accounts().ManageAccount.Handle(c) }

func TopUp(c *gin.Context) { httpapi.Redemption().Redeem.Handle(c) }

type UpdateUserSettingRequest = update_notification_preferences.UpdateUserSettingRequest

func UpdateUserSetting(c *gin.Context) { httpapi.Accounts().UpdateNotificationPreferences.Handle(c) }

// BulkUpdateUserGroupRequest is the request body for POST /api/user/group/batch.
type BulkUpdateUserGroupRequest = change_group.BulkUpdateUserGroupRequest

// BulkUpdateUserGroupResponse summarizes the outcome of a bulk group update.
type BulkUpdateUserGroupResponse = change_group.BulkUpdateUserGroupResponse

func BulkUpdateUserGroup(c *gin.Context) { httpapi.Accounts().ChangeGroup.Handle(c) }

package controller

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	check_in "github.com/QuantumNous/new-api/internal/accounts/check_in"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	get_checkin_status "github.com/QuantumNous/new-api/internal/accounts/get_checkin_status"
	"github.com/QuantumNous/new-api/internal/accounts/initialize_pat"
	provision_account "github.com/QuantumNous/new-api/internal/accounts/provision_account"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	checkinsettings "github.com/QuantumNous/new-api/internal/adapter/settings/checkin"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	batchstate "github.com/QuantumNous/new-api/pkg/batchstate"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Run these contracts against the same real-database fixture used by admin quota
// tests. TEST_MANAGE_USER_DIALECT selects SQLite, MySQL or PostgreSQL.
func TestAccountsPersistenceCompatibility(t *testing.T) {
	db := setupManageUserTestDB(t)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.Checkin{}, &model.Token{}, &identityrecord.TwoFA{}, &identityrecord.TwoFABackupCode{}, &identityrecord.PasskeyCredential{}, &identityrecord.AuthFlow{}, &identityrecord.UserOAuthBinding{}, &model.BatchJob{}))
	}
	oldNew, oldInvitee, oldInviter, oldUnit := common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter, common.QuotaPerUnit
	oldPayment, oldCheckin := *operation_setting.GetPaymentSetting(), *operation_setting.GetCheckinSetting()
	t.Cleanup(func() {
		common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter, common.QuotaPerUnit = oldNew, oldInvitee, oldInviter, oldUnit
		*operation_setting.GetPaymentSetting() = oldPayment
		*operation_setting.GetCheckinSetting() = oldCheckin
	})
	common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter, common.QuotaPerUnit = 50, 25, 15, 10
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	inviter := model.User{Username: "contract-inviter", Password: "hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "contract-inviter"}
	require.NoError(t, db.Create(&inviter).Error)
	user := model.User{Username: "contract-account", Password: "contract-password", Email: " CONTRACT@example.com ", Role: common.RoleCommonUser, InviterId: inviter.Id}
	require.NoError(t, accountscore.Insert(&user, inviter.Id, i18n.LangRu))
	require.NoError(t, db.First(&user, user.Id).Error)
	var userLogs, inviterLogs []string
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", user.Id).Order("id").Pluck("content", &userLogs).Error)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", inviter.Id).Order("id").Pluck("content", &inviterLogs).Error)
	assert.Equal(t, []string{"Бонус за регистрацию нового пользователя " + logger.LogQuota(50), "Бонус за использование кода приглашения " + logger.LogQuota(25)}, userLogs, "registration logs use the request language")
	assert.Equal(t, []string{"Бонус за приглашённого пользователя " + logger.LogQuota(15)}, inviterLogs)
	assert.Equal(t, 75, user.Quota)
	assert.Equal(t, "contract@example.com", user.Email)
	assert.True(t, common.ValidatePasswordAndHash("contract-password", user.Password))
	assert.Contains(t, user.GetSetting().SidebarModules, `"console"`)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 15, inviter.AffQuota)
	assert.Equal(t, 1, inviter.AffCount)

	t.Run("update omits zero group and preserves accounting", func(t *testing.T) {
		require.NoError(t, db.Model(&user).Updates(map[string]any{"quota": 1000, "used_quota": 20, "access_token": "0123456789abcdef0123456789abcdef", "group": "default"}).Error)
		before, err := accountstorage.SQL().GetUserById(user.Id, true)
		require.NoError(t, err)
		partial := model.User{Id: user.Id, DisplayName: "Updated"}
		require.NoError(t, accountscore.Update(&partial, false))
		after, err := accountstorage.SQL().GetUserById(user.Id, true)
		require.NoError(t, err)
		assert.Equal(t, "default", after.Group)
		assert.Equal(t, 1000, after.Quota)
		assert.Equal(t, 20, after.UsedQuota)
		assert.Equal(t, "0123456789abcdef0123456789abcdef", after.GetAccessToken())
		assert.Equal(t, before.AuthVersion, after.AuthVersion)
		edit := model.User{Id: user.Id, Username: user.Username, DisplayName: "Edited", Group: ""}
		require.NoError(t, accountscore.Edit(&edit, false))
		after, err = accountstorage.SQL().GetUserById(user.Id, true)
		require.NoError(t, err)
		assert.Empty(t, after.Group)
		assert.Greater(t, after.AuthVersion, before.AuthVersion)
		assert.Equal(t, 1000, after.Quota)
	})
	t.Run("affiliate transfer commits both balances", func(t *testing.T) {
		require.NoError(t, db.Model(&user).Updates(map[string]any{"quota": 1000, "aff_quota": 100}).Error)
		require.NoError(t, accountscore.TransferAffQuotaToQuota(&user, 20))
		require.NoError(t, db.First(&user, user.Id).Error)
		assert.Equal(t, 1020, user.Quota)
		assert.Equal(t, 80, user.AffQuota)
		require.Error(t, accountscore.TransferAffQuotaToQuota(&user, 90))
		require.NoError(t, db.First(&user, user.Id).Error)
		assert.Equal(t, 1020, user.Quota)
		assert.Equal(t, 80, user.AffQuota)
	})
	t.Run("checkin credits once and compensates failure", func(t *testing.T) {
		*operation_setting.GetCheckinSetting() = operation_setting.CheckinSetting{Enabled: true, MinQuota: 7, MaxQuota: 7}
		require.NoError(t, db.First(&user, user.Id).Error)
		before := user.Quota
		record, err := check_in.NewUsecase(accountscore.NewCheckinStore(), checkinsettings.Current).Execute(user.Id)
		require.NoError(t, err)
		assert.Equal(t, 7, record.QuotaAwarded)
		_, err = check_in.NewUsecase(accountscore.NewCheckinStore(), checkinsettings.Current).Execute(user.Id)
		require.Error(t, err)
		require.NoError(t, db.First(&user, user.Id).Error)
		assert.Equal(t, before+7, user.Quota)
		stats, err := get_checkin_status.NewUsecase(accountscore.NewCheckinStore()).Execute(user.Id, time.Now().Format("2006-01"))
		require.NoError(t, err)
		history, ok := stats["records"].([]accountdomain.CheckinRecord)
		require.True(t, ok, "existing Go callers must retain the public history record type")
		require.Len(t, history, 1)
		assert.Equal(t, 7, history[0].QuotaAwarded)
		assert.Equal(t, record.CheckinDate, history[0].CheckinDate)

		failing := model.User{Username: "failing-checkin", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "failing-checkin"}
		require.NoError(t, db.Create(&failing).Error)
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("accounts_fail_credit", func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "users" {
				tx.AddError(errors.New("credit unavailable"))
			}
		}))
		_, err = check_in.NewUsecase(accountscore.NewCheckinStore(), checkinsettings.Current).Execute(failing.Id)
		require.NoError(t, db.Callback().Update().Remove("accounts_fail_credit"))
		require.Error(t, err)
		var count int64
		require.NoError(t, db.Model(&model.Checkin{}).Where("user_id = ?", failing.Id).Count(&count).Error)
		assert.Zero(t, count)
	})
	t.Run("replaced provisioning errors and username conflict stay live", func(t *testing.T) {
		invalid, conflict, taken := accountdomain.ErrGetAPIInvalidRequest, accountdomain.ErrGetAPICreateConflict, accountdomain.ErrUsernameAlreadyTaken
		t.Cleanup(func() {
			accountdomain.ErrGetAPIInvalidRequest, accountdomain.ErrGetAPICreateConflict, accountdomain.ErrUsernameAlreadyTaken = invalid, conflict, taken
		})
		accountdomain.ErrGetAPIInvalidRequest = errors.New("replacement invalid")
		accountdomain.ErrGetAPICreateConflict = errors.New("replacement conflict")
		accountdomain.ErrUsernameAlreadyTaken = errors.New("replacement username")
		negative := -1
		_, err := accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleRootUser, provision_account.GetAPICreateUserRequest{Username: "negative", Password: "contract-password", InitialQuota: &negative})
		assert.ErrorIs(t, err, accountdomain.ErrGetAPIInvalidRequest)
		_, err = accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleRootUser, provision_account.GetAPICreateUserRequest{Username: user.Username, Password: "contract-password"})
		assert.ErrorIs(t, err, accountdomain.ErrGetAPICreateConflict)
		ctx, response := newRestContext(t, http.MethodPut, "/api/user/", map[string]any{"id": inviter.Id, "username": user.Username}, nil, common.RoleRootUser)
		UpdateUser(ctx)
		assert.Equal(t, http.StatusConflict, response.Code)
		assert.Equal(t, "username_already_taken", decodeRestError(t, response).Code)
	})
	t.Run("provision preserves absent and explicit zero quota", func(t *testing.T) {
		zero := 0
		for _, tc := range []struct {
			name  string
			quota *int
			want  int
		}{{"quota-default", nil, 50}, {"quota-zero", &zero, 0}} {
			result, err := accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleRootUser, provision_account.GetAPICreateUserRequest{Username: tc.name, Password: "contract-password", InitialQuota: tc.quota})
			require.NoError(t, err)
			assert.Equal(t, tc.want, result.InitialQuota)
			assert.Len(t, result.AccessToken, 32)
			saved, err := accountstorage.SQL().GetUserById(result.UserID, true)
			require.NoError(t, err)
			assert.Equal(t, tc.want, saved.Quota)
			assert.Equal(t, result.AccessToken, saved.GetAccessToken())
			assert.NotEmpty(t, saved.GetSetting().SidebarModules)
		}
	})
	t.Run("admin clear binding preserves permission and normalization", func(t *testing.T) {
		for _, tc := range []struct {
			name, binding          string
			actorRole, targetRole  int
			wantSuccess, wantClear bool
		}{
			{"root clears admin", " GITHUB ", common.RoleRootUser, common.RoleAdminUser, true, true},
			{"peer denied", "github", common.RoleAdminUser, common.RoleAdminUser, false, false},
			{"root can clear root", "github", common.RoleRootUser, common.RoleRootUser, true, true},
			{"unknown rejected", "unknown", common.RoleRootUser, common.RoleCommonUser, false, false},
			{"empty rejected", " ", common.RoleRootUser, common.RoleCommonUser, false, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				target := model.User{Username: "binding-" + strconv.Itoa(tc.targetRole) + "-" + strconv.Itoa(len(tc.name)), Password: "hash", Role: tc.targetRole, Status: common.UserStatusEnabled, GitHubId: "external-owner", AffCode: tc.name}
				require.NoError(t, db.Create(&target).Error)
				before := target.AuthVersion
				ctx, response := newRestContext(t, http.MethodDelete, "/api/user/"+strconv.Itoa(target.Id)+"/bindings/github", nil, gin.Params{{Key: "id", Value: strconv.Itoa(target.Id)}, {Key: "binding_type", Value: tc.binding}}, tc.actorRole)
				AdminClearUserBinding(ctx)
				assert.Equal(t, http.StatusOK, response.Code)
				var body struct {
					Success bool   `json:"success"`
					Message string `json:"message"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				assert.Equal(t, tc.wantSuccess, body.Success, response.Body.String())
				if tc.wantSuccess {
					assert.Equal(t, "success", body.Message)
				}
				require.NoError(t, db.First(&target, target.Id).Error)
				if tc.wantClear {
					assert.Empty(t, target.GitHubId)
				} else {
					assert.Equal(t, "external-owner", target.GitHubId)
				}
				assert.Equal(t, before, target.AuthVersion)
			})
		}
	})

	t.Run("PAT initialization preserves persisted credential and account balances", func(t *testing.T) {
		target := model.User{Username: "pat-persistence", AffCode: "pat-persistence", Password: "hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 321, UsedQuota: 9, RequestCount: 4, AuthVersion: 3}
		require.NoError(t, db.Create(&target).Error)
		request := initialize_pat.Input{UserID: target.Id, ExpectedUsername: target.Username}
		result, err := accountscore.InitializePAT().Execute(request)
		require.NoError(t, err)
		assert.Equal(t, "would_issue", result.Outcome)
		assert.Nil(t, result.AccessToken)
		require.NoError(t, db.First(&target, target.Id).Error)
		assert.Nil(t, target.AccessToken)
		assert.Nil(t, target.AccessTokenCreatedAt)
		request.Apply = true
		result, err = accountscore.InitializePAT().Execute(request)
		require.NoError(t, err)
		assert.Equal(t, "issued", result.Outcome)
		require.NotNil(t, result.AccessToken)
		issued := *result.AccessToken
		assert.Len(t, issued, 32)
		result, err = accountscore.InitializePAT().Execute(request)
		require.NoError(t, err)
		assert.Equal(t, "reused", result.Outcome)
		require.NotNil(t, result.AccessToken)
		assert.Equal(t, issued, *result.AccessToken)
		require.NoError(t, db.First(&target, target.Id).Error)
		assert.Equal(t, issued, target.GetAccessToken())
		require.NotNil(t, target.AccessTokenCreatedAt)
		assert.Positive(t, *target.AccessTokenCreatedAt)
		assert.Equal(t, 321, target.Quota)
		assert.Equal(t, 9, target.UsedQuota)
		assert.Equal(t, 4, target.RequestCount)
		assert.Equal(t, int64(3), target.AuthVersion)
		blocked := model.User{Username: "pat-blocked", AffCode: "pat-blocked", Password: "hash", Role: common.RoleCommonUser, Status: common.UserStatusDisabled}
		require.NoError(t, db.Create(&blocked).Error)
		result, err = accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: blocked.Id, ExpectedUsername: blocked.Username, Apply: true})
		require.NoError(t, err)
		assert.Equal(t, "blocked", result.State)
		assert.Equal(t, "blocked_no_pat", result.Outcome)
		assert.Nil(t, result.AccessToken)
		require.NoError(t, db.First(&blocked, blocked.Id).Error)
		assert.Nil(t, blocked.AccessToken)
		assert.Nil(t, blocked.AccessTokenCreatedAt)
	})
	t.Run("PAT post-update failure rolls back token and timestamp", func(t *testing.T) {
		target := model.User{Username: "pat-rollback", AffCode: "pat-rollback", Password: "hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 123, AuthVersion: 5}
		require.NoError(t, db.Create(&target).Error)
		forced := errors.New("forced PAT save failure")
		hits := 0
		var priorError error
		var affected int64
		require.NoError(t, db.Callback().Update().After("gorm:update").Register("pat_save_rollback", func(tx *gorm.DB) {
			values, ok := tx.Statement.Dest.(map[string]any)
			if ok && tx.Statement.Table == "users" && values["access_token"] != nil {
				hits++
				priorError, affected = tx.Error, tx.RowsAffected
				tx.AddError(forced)
			}
		}))
		t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove("pat_save_rollback")) })
		_, err := accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: target.Id, ExpectedUsername: target.Username, Apply: true})
		require.ErrorIs(t, err, accountdomain.ErrGetAPICredentialUnavailable)
		require.Equal(t, 1, hits)
		require.NoError(t, priorError)
		require.EqualValues(t, 1, affected)
		require.NoError(t, db.First(&target, target.Id).Error)
		assert.Nil(t, target.AccessToken)
		assert.Nil(t, target.AccessTokenCreatedAt)
		assert.Equal(t, 123, target.Quota)
		assert.Equal(t, int64(5), target.AuthVersion)
	})

	t.Run("hard delete retains unresolved financial owner", func(t *testing.T) {
		job := model.BatchJob{UserID: user.Id, ReservedQuota: 1, BillingStatus: "unknown", PurchaseStatus: "unknown"}
		require.NoError(t, db.Create(&job).Error)
		require.ErrorIs(t, accountscore.HardDelete(&user), batchstate.ErrConflict)
		require.NoError(t, db.First(&user, user.Id).Error)
	})
}

// The matrix runs the same test binary twice against an owned persistent DB:
// the baseline creates its schema/data, then the candidate opens and migrates it.
func TestAccountsStorageUpgradeCompatibility(t *testing.T) {
	phase := os.Getenv("TEST_ACCOUNTS_UPGRADE_PHASE")
	if phase == "" {
		t.Skip("requires the disposable database upgrade harness")
	}
	dialect := os.Getenv("TEST_MANAGE_USER_DIALECT")
	dsn := os.Getenv("TEST_ACCOUNTS_UPGRADE_DSN")
	require.NotEmpty(t, dsn)
	var driver gorm.Dialector
	switch dialect {
	case "sqlite":
		driver = sqlite.Open(dsn)
	case "mysql":
		driver = mysql.Open(dsn)
	case "postgres":
		driver = postgres.Open(dsn)
	default:
		t.Fatalf("unknown dialect %q", dialect)
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.Checkin{}))
	}
	if phase == "baseline" {
		user := model.User{Username: "upgrade-account", Password: "upgrade-hash", DisplayName: "Preserved", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 1234, UsedQuota: 17, AffCode: "upgrade-account", Setting: `{"language":"ru"}`, AuthVersion: 7}
		require.NoError(t, db.Create(&user).Error)
		require.NoError(t, db.Create(&model.Checkin{UserId: user.Id, CheckinDate: "2026-10-20", QuotaAwarded: 13}).Error)
		return
	}
	require.Equal(t, "candidate", phase)
	var user model.User
	require.NoError(t, db.Where("username = ?", "upgrade-account").First(&user).Error)
	assert.Equal(t, "Preserved", user.DisplayName)
	assert.Equal(t, 1234, user.Quota)
	assert.Equal(t, 17, user.UsedQuota)
	assert.Equal(t, int64(7), user.AuthVersion)
	assert.Nil(t, user.AccessToken)
	assert.Equal(t, "ru", user.GetSetting().Language)
	duplicate := model.User{Username: user.Username, AffCode: "different"}
	require.Error(t, db.Create(&duplicate).Error)
	require.Error(t, db.Create(&model.Checkin{UserId: user.Id, CheckinDate: "2026-10-20", QuotaAwarded: 99}).Error)
	var record model.Checkin
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&record).Error)
	assert.Equal(t, 13, record.QuotaAwarded)
}

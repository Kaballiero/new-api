package controller

import (
	usagestorage "github.com/QuantumNous/new-api/internal/app/usagestorage"

	patstorage "github.com/QuantumNous/new-api/internal/app/patstorage"

	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"

	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	auditstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/audit"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/audittest"
	usagecleanup "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	"github.com/QuantumNous/new-api/internal/app/authentication"
	authz "github.com/QuantumNous/new-api/internal/app/authorizationcore"
	subscriptionsapp "github.com/QuantumNous/new-api/internal/app/subscriptions"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	auditdto "github.com/QuantumNous/new-api/internal/audit/dto"
	audittransport "github.com/QuantumNous/new-api/internal/audit/transport"
	patdomain "github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/get_status"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/revoke_token"
	subscriptionsdomain "github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
)

func setupAccessTokenAudit(t *testing.T) (*model.User, string) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &identityrecord.UserSession{}, &model.Log{}, &model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.IsMasterNode = previousMaster
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
	})
	token := "legacy-opaque-token"
	user := &model.User{Username: "audit-owner", Password: "placeholder", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, Group: "default", AccessToken: &token, AuthVersion: 1, AffCode: "audit-owner"}
	require.NoError(t, db.Create(user).Error)
	return user, token
}

// userAccessTokenStatus reads the PAT status the way GET /api/user/token/status does.
func userAccessTokenStatus(id int) (*get_status.Output, error) {
	roles := patdomain.AuditRoles{Root: common.RoleRootUser, Common: common.RoleCommonUser, Admin: common.RoleAdminUser}
	return get_status.NewUsecase(patstorage.Persistence(), patdomain.Fingerprint, roles).Execute(get_status.Input{UserID: id})
}

// revokeUserAccessToken revokes the PAT without an HTTP audit context and
// returns the revoked generation reference.
func revokeUserAccessToken(id int) (string, error) {
	output, err := revoke_token.NewUsecase(patstorage.Persistence(), patdomain.Fingerprint, nil).Execute(revoke_token.Input{UserID: id})
	return output.TokenRef, err
}

func auditRequest(router http.Handler, method, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(`{"secret":"body-must-not-be-logged"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "audit-test-client")
	request.RemoteAddr = "192.0.2.8:4567"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAuditRoleSnapshotSurvivesActorChanges(t *testing.T) {
	user, pat := setupAccessTokenAudit(t)
	user.Role = common.RoleRootUser
	require.NoError(t, model.DB.Model(user).Update("role", user.Role).Error)
	router := gin.New()
	router.Use(middleware.RequestId(), middleware.AccessTokenAudit())
	router.POST("/change-role", middleware.UserAuth(), func(c *gin.Context) {
		recordLoginAudit(user, c)
		recordUserSecurityAudit(c, user.Id, "user.security_verify", nil)
		subscriptionsapp.NewResetAudit().RecordUserLogs(c, &subscriptionsdomain.ResetResult{ResetCount: 1, PlanId: 1, PlanTitle: "Plan", AffectedUserIds: []int{999}}, audittransport.Operator(c))
		require.NoError(t, model.DB.Model(user).Update("role", common.RoleAdminUser).Error)
		c.Status(200)
	})
	require.Equal(t, http.StatusOK, auditRequest(router, "POST", "/change-role", pat).Code)
	entries, total, err := readAuditEvents(auditFilter{}, 0, 20, common.RoleAdminUser)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, entries)
	status, err := userAccessTokenStatus(user.Id)
	require.NoError(t, err)
	assert.Nil(t, status.LastUsedAt, "a root request must not leak through the last-use summary after demotion")
	require.NoError(t, model.DB.Unscoped().Delete(user).Error)
	entries, total, err = readAuditEvents(auditFilter{}, 0, 20, common.RoleRootUser)
	require.NoError(t, err)
	assert.EqualValues(t, 4, total)
	for _, entry := range entries {
		assert.Equal(t, common.RoleRootUser, entry.ActorRole)
	}
}

func TestSecurityAndOperationEventsUseAuditTable(t *testing.T) {
	user, _ := setupAccessTokenAudit(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/event", nil)
	c.Set("id", user.Id)
	c.Set("username", user.Username)
	c.Set("role", user.Role)
	c.Set(common.RequestIdKey, "correlated-request")
	recordLoginAudit(user, c)
	for _, action := range []string{"user.passkey_register", "user.passkey_delete", "user.2fa_setup", "user.2fa_enable", "user.2fa_disable_self", "user.2fa_backup_codes", "user.security_verify"} {
		recordUserSecurityAudit(c, user.Id, action, nil)
	}
	recordManageAudit(c, "option.update", map[string]any{"key": "safe"})
	subscriptionsapp.NewResetAudit().RecordUserLogs(c, &subscriptionsdomain.ResetResult{ResetCount: 1, PlanId: 1, PlanTitle: "Plan", AffectedUserIds: []int{user.Id}}, &auditdomain.Operator{ID: user.Id})
	for _, typ := range []int{model.LogTypeTopup, model.LogTypeConsume, model.LogTypeRefund, model.LogTypeSystem} {
		usagestorage.Writer().RecordLog(user.Id, typ, "business entry")
	}
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&audits).Error)
	assert.Len(t, audits, 10)
	for _, entry := range audits {
		assert.Equal(t, "correlated-request", entry.RequestId)
		assert.Equal(t, user.Role, entry.ActorRole)
	}
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Find(&logs).Error)
	assert.Len(t, logs, 4)
	_, err := (usagecleanup.Cleanup{DB: func() *gorm.DB { return model.LOG_DB }}).DeleteOldLogBatch(context.Background(), time.Now().Unix()+1, 100)
	require.NoError(t, err)
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.AuditLog{}).Count(&count).Error)
	assert.EqualValues(t, 10, count)
}

func TestPersonalAccessTokenRevocationRollsBackOnWriteFailure(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	token := "rollback-pat-credential"
	require.NoError(t, patstorage.Persistence().Update(user.Id, token))
	before, err := userAccessTokenStatus(user.Id)
	require.NoError(t, err)
	require.NotNil(t, before.CreatedAt)
	writeFailure := errors.New("personal access token write failure")
	db := model.DB
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("pat:fail_revoke", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			_ = tx.AddError(writeFailure)
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove("pat:fail_revoke")) })
	ref, err := revokeUserAccessToken(user.Id)
	require.ErrorIs(t, err, writeFailure)
	assert.Equal(t, patdomain.Fingerprint(token), ref, "the Go API retains the attempted generation reference on failure")
	after, err := userAccessTokenStatus(user.Id)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a failed revoke must retain the original PAT and creation time")
	loaded, err := authentication.NewPATAuth().Execute(token)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, user.Id, loaded.Id)
}

type auditFilter struct {
	SelfView                                                 bool
	UserId                                                   int
	Username, Category, TokenRef, ExcludeTokenRef, RequestId string
	StartTimestamp, EndTimestamp                             int64
	Success                                                  *bool
}

func readAuditEvents(filter auditFilter, start, limit, role int) ([]*auditdto.Event, int64, error) {
	store := auditstorage.NewStore(func() *gorm.DB { return model.LOG_DB }, nil)
	query := auditdomain.Query{UserId: filter.UserId, Username: filter.Username, Category: filter.Category, TokenRef: filter.TokenRef,
		ExcludeTokenRef: filter.ExcludeTokenRef, RequestId: filter.RequestId, StartTimestamp: filter.StartTimestamp,
		EndTimestamp: filter.EndTimestamp, Success: filter.Success, Offset: start, Limit: limit}
	return store.List(query, auditdomain.EventVisibility(role, auditdomain.Roles{Common: 1, Admin: 10, Root: 100}, filter.SelfView))
}

func newAuditTestDatabase(t *testing.T, kind, dsn string) (*gorm.DB, string) {
	return audittest.NewDatabase(t, kind, dsn)
}

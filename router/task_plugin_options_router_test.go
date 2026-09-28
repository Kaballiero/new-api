package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetTaskPluginOptionsAdminForbiddenRootAllowed(t *testing.T) {
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() { common.IsMasterNode = wasMaster })

	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		id         int
		role       int
		wantStatus int
	}{
		{name: "admin", id: 2, role: common.RoleAdminUser, wantStatus: http.StatusForbidden},
		{name: "root", id: 1, role: common.RoleRootUser, wantStatus: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodGet, "/api/task_plugin_options", nil)
			context.Set("id", testCase.id)
			context.Set("role", testCase.role)
			middleware.RequirePermission(authz.TaskPluginBind)(context)
			if !context.IsAborted() {
				controller.GetTaskPluginOptions(context)
			}
			assert.Equal(t, testCase.wantStatus, recorder.Code)
		})
	}
}

func TestEffectivePricingPreviewRequiresRoot(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis := common.RedisEnabled
	oldRateLimit := common.GlobalApiRateLimitEnable
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.ModelCostFXRow{}))
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = false
	oldQuotaPerUnit := common.QuotaPerUnit
	oldExchangeRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	oldGroupRatio := ratio_setting.GroupRatio2JSONString()
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	now := time.Now().Unix()
	require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
		Source: "cbr-xml-daily.ru", EffectiveAt: now, FetchedAt: now, Rates: map[string]float64{"USD": 90},
	}, 0))
	model.InvalidatePricingCache()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled = oldRedis
		common.GlobalApiRateLimitEnable = oldRateLimit
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = oldExchangeRate
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatio))
		model.InvalidatePricingCache()
		require.NoError(t, sqlDB.Close())
	})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	for _, tc := range []struct {
		name   string
		role   int
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "user", role: common.RoleCommonUser, status: http.StatusForbidden},
		{name: "admin", role: common.RoleAdminUser, status: http.StatusForbidden},
		// Invalid group reaches handler validation only after successful root auth.
		{name: "root", role: common.RoleRootUser, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/option/effective_pricing?user_group=", nil)
			if tc.role != 0 {
				token := "preview-test-" + tc.name
				user := model.User{Id: 9200 + tc.role, Username: "preview-" + tc.name, Role: tc.role, Group: "default", AffCode: tc.name, Status: common.UserStatusEnabled, AccessToken: &token}
				require.NoError(t, db.Create(&user).Error)
				request.Header.Set("Authorization", "Bearer "+token)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.role == common.RoleRootUser {
				assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
				assert.Contains(t, recorder.Body.String(), "invalid user pricing group")
			}
		})
	}

	for _, tc := range []struct {
		name   string
		role   int
		status int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "user", role: common.RoleCommonUser, status: http.StatusForbidden},
		{name: "admin", role: common.RoleAdminUser, status: http.StatusForbidden},
		{name: "root", role: common.RoleRootUser, status: http.StatusOK},
	} {
		t.Run("group_effective_"+tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/pricing/effective", nil)
			if tc.role != 0 {
				request.Header.Set("Authorization", "Bearer preview-test-"+tc.name)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			assert.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.role == common.RoleRootUser {
				assert.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
				assert.Contains(t, recorder.Body.String(), "\"user_group\":\"default\"")
			}
		})
	}
}

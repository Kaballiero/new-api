package systemruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/system/purge_old_logs"

	usagecleanup "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"

	"github.com/QuantumNous/new-api/internal/system/list_system_instances"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	systemstore "github.com/QuantumNous/new-api/internal/adapter/gorm/system"
	systemrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	settings "github.com/QuantumNous/new-api/internal/app/settings"
	core "github.com/QuantumNous/new-api/internal/app/system"
	"github.com/QuantumNous/new-api/internal/contracttest/pgfixture"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/internal/system/complete_setup"
	"github.com/QuantumNous/new-api/internal/system/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemSetupPersistencePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	taskfixture.WithSQLite(t)
	db := pgfixture.Open(t, dsn)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypePostgreSQL)
	var version string
	require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Log("database version:", version)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &systemrecord.Setup{}))
	oldSetup := constant.Setup
	constant.Setup = false
	t.Cleanup(func() { constant.Setup = oldSetup })
	settings.Initialize()
	require.NoError(t, i18n.Init())
	body := `{"username":"bootstrap","password":"fixture-password-8","confirmPassword":"fixture-password-8","SelfUseModeEnabled":true,"DemoSiteEnabled":false}`
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest("POST", "/api/setup", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	NewHTTP().CompleteSetup.Handle(c)
	assert.Equal(t, 200, r.Code)
	assert.JSONEq(t, `{"success":true,"message":"System initialization succeeded"}`, r.Body.String())
	var root model.User
	require.NoError(t, db.Where("role = ?", common.RoleRootUser).First(&root).Error)
	assert.Equal(t, 100000000, root.Quota)
	assert.Nil(t, root.AccessToken)
	require.True(t, common.ValidatePasswordAndHash("fixture-password-8", root.Password))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &systemrecord.Setup{}))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &systemrecord.Setup{}))
	var options []model.Option
	require.NoError(t, db.Order("key").Find(&options).Error)
	values := map[string]string{}
	for _, v := range options {
		values[v.Key] = v.Value
	}
	assert.Equal(t, "true", values["SelfUseModeEnabled"])
	assert.Equal(t, "false", values["DemoSiteEnabled"])
	store := systemstore.Store{DB: func() *gorm.DB { return db }}
	require.NotNil(t, store.SetupTime())
	constant.Setup = false
	CheckSetup()
	assert.True(t, constant.Setup)
	r = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(r)
	c.Request = httptest.NewRequest("POST", "/api/setup", strings.NewReader("invalid"))
	NewHTTP().CompleteSetup.Handle(c)
	assert.JSONEq(t, `{"success":false,"message":"The system has already been initialized"}`, r.Body.String())
	var count int64
	require.NoError(t, db.Model(&model.User{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
func TestLogCleanupPostgres(t *testing.T) {
	mainDSN, logDSN := os.Getenv("TEST_POSTGRES_DSN"), os.Getenv("TEST_LOG_POSTGRES_DSN")
	if mainDSN == "" || logDSN == "" {
		t.Skip("TEST_POSTGRES_DSN and TEST_LOG_POSTGRES_DSN are required")
	}
	taskfixture.WithSQLite(t)
	mainDB, logDB := pgfixture.Open(t, mainDSN), pgfixture.Open(t, logDSN)
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	for _, db := range []*gorm.DB{mainDB, logDB} {
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Log("database version:", version)
	}
	require.NoError(t, mainDB.AutoMigrate(&systemrecord.SystemTask{}, &systemrecord.SystemTaskLock{}, &usagerecord.Log{}))
	require.NoError(t, logDB.AutoMigrate(&usagerecord.Log{}))
	require.NoError(t, mainDB.Create(&usagerecord.Log{Id: 1, CreatedAt: 1, Content: "main sentinel"}).Error)
	require.NoError(t, logDB.Create(&[]usagerecord.Log{
		{Id: 1, CreatedAt: 99}, {Id: 2, CreatedAt: 100}, {Id: 3, CreatedAt: 101}, {Id: 4, CreatedAt: 99}, {Id: 5, CreatedAt: 99},
	}).Error)
	cleanup := usagecleanup.Cleanup{DB: func() *gorm.DB { return logDB }}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cleanup.DeleteOldLogBatch(cancelled, 100, 2)
	require.ErrorIs(t, err, context.Canceled)
	remaining, err := cleanup.CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.Equal(t, int64(3), remaining)
	// Preserve the legacy PostgreSQL DELETE builder: Limit does not bound this statement.
	deleted, err := cleanup.DeleteOldLogBatch(context.Background(), 100, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), deleted)
	remaining, err = cleanup.CountOldLog(context.Background(), 100)
	require.NoError(t, err)
	assert.Zero(t, remaining)
	require.NoError(t, logDB.Create(&usagerecord.Log{Id: 1, CreatedAt: 99}).Error)
	runtime := core.Default()
	task, err := runtime.StartCleanup.Execute(100)
	require.NoError(t, err)
	task, won, err := runtime.Store.ClaimSystemTask(task.ID, task.Type, "cleanup", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, runtime.Cleanup.Execute(context.Background(), purge_old_logs.Input{Task: task, RunnerID: "cleanup"}))
	saved, err := runtime.Store.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.SystemTaskStatusSucceeded, saved.Status)
	assert.JSONEq(t, `{"deleted_count":1}`, saved.Result)
	var rows []usagerecord.Log
	require.NoError(t, logDB.Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(100), rows[0].CreatedAt)
	var sentinel usagerecord.Log
	require.NoError(t, mainDB.First(&sentinel).Error)
	assert.Equal(t, "main sentinel", sentinel.Content)
}

type instanceReadFixture struct {
	read func() ([]domain.Instance, error)
}

func (f instanceReadFixture) ListSystemInstances() ([]domain.Instance, error) { return f.read() }
func TestInstancesReadClockAfterQuery(t *testing.T) {
	read, clock := false, false
	store := instanceReadFixture{read: func() ([]domain.Instance, error) {
		read = true
		return []domain.Instance{{NodeName: "node", LastSeenAt: 1, Info: "{}"}}, nil
	}}
	handler := list_system_instances.NewHandler(store, func() int64 { require.True(t, read); clock = true; return 92 }, func(raw string) any { require.True(t, clock); assert.Equal(t, "{}", raw); return map[string]any{} })
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	handler.Handle(c)
	assert.JSONEq(t, `{"success":true,"message":"","data":[{"node_name":"node","status":"stale","stale_after_seconds":90,"started_at":0,"last_seen_at":1,"info":{}}]}`, response.Body.String())
}

type failingSetup struct{ err error }

func (s failingSetup) Execute(complete_setup.Input) error { return s.err }

func TestCompleteSetupErrorResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	cause := errors.New("fixture cause")
	for _, tc := range []struct {
		name    string
		err     error
		message string
	}{
		{"username", domain.ErrSetupUsername, "用户名长度不能超过12个字符"},
		{"mismatch", domain.ErrSetupPasswordMismatch, "两次输入的密码不一致"},
		{"password", fmt.Errorf("%w: %w", domain.ErrSetupPassword, cause), "fixture cause"},
		{"hash", fmt.Errorf("%w: %w", domain.ErrSetupHash, cause), "系统错误: fixture cause"},
		{"root", fmt.Errorf("%w: %w", domain.ErrSetupRoot, cause), "创建管理员账号失败: fixture cause"},
		{"self", fmt.Errorf("%w: %w", domain.ErrSetupSelfMode, cause), "保存自用模式设置失败: fixture cause"},
		{"demo", fmt.Errorf("%w: %w", domain.ErrSetupDemoMode, cause), "保存演示站点模式设置失败: fixture cause"},
		{"persist", fmt.Errorf("%w: %w", domain.ErrSetupPersist, cause), "系统初始化失败: fixture cause"},
		{"unexpected", cause, "系统错误: fixture cause"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("POST", "/api/setup", strings.NewReader(`{}`))
			c.Request.Header.Set("Accept-Language", "zh-CN")
			c.Set(string(constant.ContextKeyLanguage), "zh-CN")
			handler := complete_setup.NewHandler(failingSetup{err: tc.err}, func() bool { return false }, func() bool { return false })
			assert.NotPanics(t, func() { handler.Handle(c) })
			assert.Equal(t, 200, response.Code)
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.False(t, body.Success)
			assert.Equal(t, tc.message, body.Message)
		})
	}
}

func TestSystemMessagesKeepChineseBytes(t *testing.T) {
	require.NoError(t, i18n.Init())
	old := constant.Setup
	constant.Setup = true
	t.Cleanup(func() { constant.Setup = old })
	for _, tc := range []struct{ lang, want string }{
		{"en", "The system has already been initialized"},
		{"ru", "Система уже инициализирована"},
		{"zh-CN", "系统已经初始化完成"},
		{"zh-TW", "系统已经初始化完成"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("POST", "/api/setup", strings.NewReader("invalid"))
			c.Request.Header.Set("Accept-Language", tc.lang)
			NewHTTP().CompleteSetup.Handle(c)
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.False(t, body.Success)
			assert.Equal(t, tc.want, body.Message)
		})
	}
}

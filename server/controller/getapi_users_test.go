package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/accounts/credit_getapi_quota"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"
	initialize_pat "github.com/QuantumNous/new-api/internal/accounts/initialize_pat"
	provision_account "github.com/QuantumNous/new-api/internal/accounts/provision_account"
	accountscore "github.com/QuantumNous/new-api/internal/app/accountscore"
	"github.com/QuantumNous/new-api/internal/app/authentication"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAPICustomCreateAndInitializeRetainsPAT(t *testing.T) {
	previousQuota := common.QuotaForNewUser
	common.QuotaForNewUser = 50_000
	t.Cleanup(func() { common.QuotaForNewUser = previousQuota })

	principal, adminPAT := setupAccessTokenAudit(t)
	cfg, _ := common.Marshal([]map[string]interface{}{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision", "getapi.users.initialize-pat"}}})
	t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
	r := gin.New()
	r.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
	r.POST("/api/getapi/users/:user_id/pat", middleware.GetAPIAuth("getapi.users.initialize-pat"), InitializeGetAPIPAT)
	req := func(path, body, cap, key string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+adminPAT)
		if key != "" {
			q.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	created := req("/api/getapi/users", `{"username":"retain-user","password":"safe-password","display_name":"Retain"}`, "", "attempt-1")
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var env struct {
		Data struct {
			UserID       int    `json:"user_id"`
			AccessToken  string `json:"access_token"`
			InitialQuota int    `json:"initial_quota"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &env))
	assert.Equal(t, common.QuotaForNewUser, env.Data.InitialQuota)
	old := env.Data.AccessToken
	var legacyUser model.User
	require.NoError(t, model.DB.First(&legacyUser, env.Data.UserID).Error)
	assert.Equal(t, "default", legacyUser.Group)
	assert.Equal(t, env.Data.InitialQuota, legacyUser.Quota)
	init := req(fmt.Sprintf("/api/getapi/users/%d/pat", env.Data.UserID), `{"expected_username":"retain-user","apply":false}`, "", "")
	assert.Equal(t, http.StatusOK, init.Code)
	assert.Contains(t, init.Body.String(), "would_reuse")
	assert.Contains(t, init.Body.String(), `"access_token":null`)
	apply := req(fmt.Sprintf("/api/getapi/users/%d/pat", env.Data.UserID), `{"expected_username":"retain-user","apply":true}`, "", "")
	assert.Equal(t, http.StatusOK, apply.Code)
	assert.Contains(t, apply.Body.String(), old)
	user := model.User{Id: env.Data.UserID, Status: common.UserStatusDisabled}
	require.NoError(t, accountscore.Update(&user, false))
	blocked := req(fmt.Sprintf("/api/getapi/users/%d/pat", user.Id), `{"expected_username":"retain-user","apply":true}`, "", "")
	assert.Equal(t, http.StatusOK, blocked.Code)
	assert.Contains(t, blocked.Body.String(), old)
}
func TestGetAPICreateRejectsUnknownNullDuplicate(t *testing.T) {
	principal, pat := setupAccessTokenAudit(t)
	cfg, _ := common.Marshal([]map[string]interface{}{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
	t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
	r := gin.New()
	r.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
	for _, body := range []string{`{"username":"x","password":"safe-password","display_name":"x","external_account_id":"bad"}`, `{"username":"x","password":null,"display_name":"x"}`, `{"username":"x","username":"y","password":"safe-password","display_name":"x"}`, `{"username":"x","password":"safe-password","display_name":"x","group":null}`, `{"username":"x","password":"safe-password","display_name":"x","group":""}`} {
		q := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+pat)
		q.Header.Set("Idempotency-Key", "x")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
}

func TestGetAPIProvisionPersistsRequestedGroupAndRejectsUnknownGroup(t *testing.T) {
	withTestGroupRatios(t, map[string]float64{"vip": 1})
	principal, pat := setupAccessTokenAudit(t)
	cfg, err := common.Marshal([]map[string]interface{}{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
	require.NoError(t, err)
	t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
	r := gin.New()
	r.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
	post := func(body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+pat)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	suffix := fmt.Sprintf("%d", common.GetTimestamp())
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	username := "grp" + suffix
	created := post(fmt.Sprintf(`{"username":%q,"password":"safe-password","display_name":"Group User","group":"vip"}`, username))
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var envelope struct {
		Data struct {
			UserID      int    `json:"user_id"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.AccessToken)
	var persisted model.User
	require.NoError(t, model.DB.First(&persisted, envelope.Data.UserID).Error)
	assert.Equal(t, "vip", persisted.Group)
	assert.Equal(t, envelope.Data.AccessToken, persisted.GetAccessToken())

	invalidUsername := "bad" + suffix
	invalid := post(fmt.Sprintf(`{"username":%q,"password":"safe-password","display_name":"Invalid","group":"does-not-exist"}`, invalidUsername))
	assert.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", invalidUsername).Count(&count).Error)
	assert.Zero(t, count)
}

func TestGetAPIProvisionGroupLengthMatchesVarcharCharacters(t *testing.T) {
	validGroup := strings.Repeat("界", 64)
	tooLongGroup := strings.Repeat("界", 65)
	withTestGroupRatios(t, map[string]float64{validGroup: 1, tooLongGroup: 1})
	principal, pat := setupAccessTokenAudit(t)
	cfg, err := common.Marshal([]map[string]interface{}{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
	require.NoError(t, err)
	t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
	r := gin.New()
	r.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
	post := func(body string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+pat)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	suffix := fmt.Sprintf("%d", common.GetTimestamp())
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	validUsername := "u" + suffix
	valid := post(fmt.Sprintf(`{"username":%q,"password":"safe-password","display_name":"Unicode Group","group":%q}`, validUsername, validGroup))
	require.Equal(t, http.StatusCreated, valid.Code, valid.Body.String())
	var envelope struct {
		Data struct {
			UserID      int    `json:"user_id"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(valid.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.AccessToken)
	var persisted model.User
	require.NoError(t, model.DB.First(&persisted, envelope.Data.UserID).Error)
	assert.Equal(t, validGroup, persisted.Group)
	assert.Equal(t, envelope.Data.AccessToken, persisted.GetAccessToken())

	tooLongUsername := "i" + suffix
	invalid := post(fmt.Sprintf(`{"username":%q,"password":"safe-password","display_name":"Invalid Group","group":%q}`, tooLongUsername, tooLongGroup))
	assert.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", tooLongUsername).Count(&count).Error)
	assert.Zero(t, count)
}

func withTestGroupRatios(t *testing.T, extra map[string]float64) {
	t.Helper()
	previous := ratio_setting.GetGroupRatioCopy()
	updated := make(map[string]float64, len(previous)+len(extra))
	for group, ratio := range previous {
		updated[group] = ratio
	}
	for group, ratio := range extra {
		updated[group] = ratio
	}
	encoded, err := common.Marshal(updated)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(string(encoded)))
	t.Cleanup(func() {
		encodedPrevious, marshalErr := common.Marshal(previous)
		if marshalErr != nil {
			t.Errorf("failed to restore group ratios: %v", marshalErr)
			return
		}
		if restoreErr := ratio_setting.UpdateGroupRatioByJSONString(string(encodedPrevious)); restoreErr != nil {
			t.Errorf("failed to restore group ratios: %v", restoreErr)
		}
	})
}

func TestGetAPIPATSurvivesNativeBlockEnableAndIsDeniedWhileBlocked(t *testing.T) {
	principal, adminPAT := setupAccessTokenAudit(t)
	config, err := common.Marshal([]map[string]interface{}{{"integration_id": "retain", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
	require.NoError(t, err)
	t.Setenv("GETAPI_INTEGRATIONS", string(config))
	custom := gin.New()
	custom.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
	create := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(`{"username":"native-retain","password":"safe-password","display_name":"Native Retain"}`))
	create.Header.Set("Authorization", "Bearer "+adminPAT)
	created := httptest.NewRecorder()
	custom.ServeHTTP(created, create)
	require.Equal(t, http.StatusCreated, created.Code, created.Body.String())
	var envelope struct {
		Data struct {
			UserID      int    `json:"user_id"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Data.AccessToken)
	oldPAT := envelope.Data.AccessToken
	native := gin.New()
	native.POST("/api/user/manage", middleware.AdminAuth(), ManageUser)
	native.GET("/probe", middleware.UserAuth(), func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"success": true}) })
	manage := func(action string) {
		body, marshalErr := common.Marshal(ManageRequest{Id: envelope.Data.UserID, Action: action})
		require.NoError(t, marshalErr)
		req := httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+adminPAT)
		response := httptest.NewRecorder()
		native.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	probe := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		native.ServeHTTP(response, req)
		return response.Code
	}
	require.Equal(t, http.StatusOK, probe(oldPAT))
	manage("disable")
	require.Equal(t, http.StatusUnauthorized, probe(oldPAT))
	var blocked model.User
	require.NoError(t, model.DB.First(&blocked, envelope.Data.UserID).Error)
	require.Equal(t, oldPAT, blocked.GetAccessToken())
	manage("enable")
	require.Equal(t, http.StatusOK, probe(oldPAT))
	var enabled model.User
	require.NoError(t, model.DB.First(&enabled, envelope.Data.UserID).Error)
	require.Equal(t, oldPAT, enabled.GetAccessToken())
	_, err = revokeUserAccessToken(envelope.Data.UserID)
	require.NoError(t, err)
	validated, err := authentication.NewPATAuth().Execute(oldPAT)
	require.NoError(t, err)
	assert.Nil(t, validated)
	var revoked model.User
	require.NoError(t, model.DB.First(&revoked, envelope.Data.UserID).Error)
	assert.Empty(t, revoked.GetAccessToken())
}

func TestGetAPIInitializePATMatrixAndErrors(t *testing.T) {
	_, _ = setupAccessTokenAudit(t)
	suffix := fmt.Sprintf("%d", common.GetTimestamp())
	active := model.User{Username: "init-active-" + suffix, Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "init-active-" + suffix}
	blocked := model.User{Username: "init-blocked-" + suffix, Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, AffCode: "init-blocked-" + suffix}
	admin := model.User{Username: "init-admin-" + suffix, Password: "placeholder", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "init-admin-" + suffix}
	require.NoError(t, model.DB.Create(&active).Error)
	require.NoError(t, model.DB.Create(&blocked).Error)
	require.NoError(t, model.DB.Create(&admin).Error)
	dry, err := accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: active.Id, ExpectedUsername: active.Username})
	require.NoError(t, err)
	assert.Equal(t, "would_issue", dry.Outcome)
	assert.Nil(t, dry.AccessToken)
	issued, err := accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: active.Id, ExpectedUsername: active.Username, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, "issued", issued.Outcome)
	require.NotNil(t, issued.AccessToken)
	reuse, err := accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: active.Id, ExpectedUsername: active.Username})
	require.NoError(t, err)
	assert.Equal(t, "would_reuse", reuse.Outcome)
	assert.Nil(t, reuse.AccessToken)
	blockedResult, err := accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: blocked.Id, ExpectedUsername: blocked.Username, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, "blocked_no_pat", blockedResult.Outcome)
	assert.Nil(t, blockedResult.AccessToken)
	_, err = accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: admin.Id, ExpectedUsername: admin.Username})
	assert.ErrorIs(t, err, accountdomain.ErrGetAPITargetRoleDenied)
	_, err = accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: active.Id, ExpectedUsername: "wrong"})
	assert.ErrorIs(t, err, accountdomain.ErrGetAPIUsernameMismatch)
	_, err = accountscore.InitializePAT().Execute(initialize_pat.Input{UserID: 999999, ExpectedUsername: "missing"})
	assert.ErrorIs(t, err, accountdomain.ErrGetAPIAccountNotFound)
}

func TestGetAPICustomCreateConcurrentUsernameConflict(t *testing.T) {
	_, _ = setupAccessTokenAudit(t)
	suffix := fmt.Sprintf("%d", common.GetTimestamp())
	req := provision_account.GetAPICreateUserRequest{Username: "concurrent-" + suffix, Password: "safe-password", DisplayName: "Concurrent"}
	results := make([]*provision_account.GetAPICreateCredential, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range 2 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			results[i], errs[i] = accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleAdminUser, req)
		}(i)
	}
	close(start)
	group.Wait()
	if errs[0] == nil {
		require.Error(t, errs[1])
		assert.True(t, errors.Is(errs[1], accountdomain.ErrGetAPICreateConflict) || errors.Is(errs[1], accountdomain.ErrGetAPICredentialUnavailable))
	} else {
		require.Error(t, errs[0])
		assert.True(t, errors.Is(errs[0], accountdomain.ErrGetAPICreateConflict) || errors.Is(errs[0], accountdomain.ErrGetAPICredentialUnavailable))
		assert.NoError(t, errs[1])
	}
	var persisted int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", req.Username).Count(&persisted).Error)
	assert.Equal(t, int64(1), persisted)
	_, err := accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleAdminUser, req)
	assert.ErrorIs(t, err, accountdomain.ErrGetAPICreateConflict)
}

func TestGetAPIProvisionInitialQuota(t *testing.T) {
	for _, database := range []struct {
		name, env string
		kind      common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(database.name, func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured")
			}
			principal, pat := setupAccessTokenAudit(t)
			if database.env != "" {
				db, _ := newAuditTestDatabase(t, database.name, dsn)
				require.NoError(t, db.AutoMigrate(&model.User{}, &model.AuditLog{}))
				model.DB, model.LOG_DB = db, db
				common.SetDatabaseTypes(database.kind, database.kind)
				principal.Id = 0
				require.NoError(t, db.Create(principal).Error)
			}
			versionSQL := "SELECT version()"
			if database.name == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, model.DB.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)
			previousQuota := common.QuotaForNewUser
			common.QuotaForNewUser = 50_000
			t.Cleanup(func() { common.QuotaForNewUser = previousQuota })
			config, err := common.Marshal([]map[string]any{{"integration_id": "quota-test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
			require.NoError(t, err)
			t.Setenv("GETAPI_INTEGRATIONS", string(config))
			router := gin.New()
			router.POST("/api/getapi/users", middleware.GetAPIAuth("getapi.users.provision"), ProvisionGetAPIUser)
			post := func(body, token string) *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(body))
				request.Header.Set("Authorization", "Bearer "+token)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				return response
			}
			for index, test := range []struct {
				name, field string
				quota       int
				status      int
			}{
				{"omitted", "", 50_000, http.StatusCreated},
				{"zero", `,"initial_quota":0`, 0, http.StatusCreated},
				{"positive_with_group", `,"initial_quota":12345,"group":"default"`, 12345, http.StatusCreated},
				{"maximum", `,"initial_quota":2147483647`, common.MaxQuota, http.StatusCreated},
				{"negative", `,"initial_quota":-1`, 0, http.StatusBadRequest},
				{"fraction", `,"initial_quota":1.5`, 0, http.StatusBadRequest},
				{"null", `,"initial_quota":null`, 0, http.StatusBadRequest},
				{"string", `,"initial_quota":"1"`, 0, http.StatusBadRequest},
				{"boolean", `,"initial_quota":false`, 0, http.StatusBadRequest},
				{"too_large", `,"initial_quota":2147483648`, 0, http.StatusBadRequest},
				{"overflow", `,"initial_quota":9223372036854775808`, 0, http.StatusBadRequest},
				{"duplicate", `,"initial_quota":0,"initial_quota":1`, 0, http.StatusBadRequest},
			} {
				t.Run(test.name, func(t *testing.T) {
					username := fmt.Sprintf("quota-user-%d", index)
					body := fmt.Sprintf(`{"username":%q,"password":"safe-password","display_name":"Quota"%s}`, username, test.field)
					response := post(body, pat)
					require.Equal(t, test.status, response.Code, response.Body.String())
					var count int64
					require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", username).Count(&count).Error)
					if test.status != http.StatusCreated {
						assert.Zero(t, count)
						return
					}
					var envelope struct {
						Data provision_account.GetAPICreateCredential `json:"data"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &envelope))
					assert.Equal(t, test.quota, envelope.Data.InitialQuota)
					var user model.User
					require.NoError(t, model.DB.First(&user, envelope.Data.UserID).Error)
					assert.Equal(t, test.quota, user.Quota)
					assert.Equal(t, envelope.Data.AccessToken, user.GetAccessToken())
					replay := post(body, pat)
					assert.Equal(t, http.StatusConflict, replay.Code, replay.Body.String())
					require.NoError(t, model.DB.First(&user, envelope.Data.UserID).Error)
					assert.Equal(t, test.quota, user.Quota)
				})
			}
			deniedBody := `{"username":"quota-denied","password":"safe-password","display_name":"Denied","initial_quota":123}`
			denied := post(deniedBody, "invalid-token")
			assert.Equal(t, http.StatusUnauthorized, denied.Code)
			t.Setenv("GETAPI_INTEGRATIONS", "[]")
			denied = post(deniedBody, pat)
			assert.Equal(t, http.StatusForbidden, denied.Code)
			var deniedCount int64
			require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "quota-denied").Count(&deniedCount).Error)
			assert.Zero(t, deniedCount)
			quota := 123
			_, err = accountscore.NewProvision(accountscore.GenerateDefaultSidebarConfigForRole).Execute(common.RoleCommonUser, provision_account.GetAPICreateUserRequest{Username: "quota-rollback", Password: "safe-password", InitialQuota: &quota})
			require.ErrorIs(t, err, accountdomain.ErrGetAPICapabilityDenied)
			require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "quota-rollback").Count(&deniedCount).Error)
			assert.Zero(t, deniedCount)
		})
	}
}

// Exercise the same HTTP and persistence contracts against each real dialect.
// The database helper creates isolated loopback databases for server engines.
func TestGetAPIQuotaCredits(t *testing.T) {
	for _, tc := range []struct {
		name, env string
		typ       common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" && os.Getenv(tc.env) == "" {
				t.Skip(tc.env + " is not configured")
			}
			principal, pat := setupAccessTokenAudit(t)
			var db *gorm.DB
			if tc.name == "sqlite" {
				var err error
				db, err = gorm.Open(sqlite.Open("file:"+t.TempDir()+"/credits.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(OFF)"), &gorm.Config{})
				require.NoError(t, err)
			} else {
				db, _ = newAuditTestDatabase(t, tc.name, os.Getenv(tc.env))
			}
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			// Two independent connections exercise the actual database lock boundary.
			conn.SetMaxOpenConns(4)
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.AuditLog{}))
			principal.Id = 0
			require.NoError(t, db.Create(principal).Error)
			target := model.User{Username: "credit-target", Password: "placeholder", AffCode: "credit-target", Quota: 100}
			other := model.User{Username: "credit-other", Password: "placeholder", AffCode: "credit-other", Quota: 200}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&other).Error)
			// Upgrade an existing users table, then restart migration twice. Preserve data
			// and a durable receipt, including its unique operation key.
			require.NoError(t, db.AutoMigrate(&model.GetAPIQuotaCredit{}))
			seed := model.GetAPIQuotaCredit{OperationKey: []byte("historical"), UserID: target.Id, Quota: 1, CreatedAt: 1}
			require.NoError(t, db.Create(&seed).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&model.GetAPIQuotaCredit{}))
			}
			require.Error(t, db.Create(&model.GetAPIQuotaCredit{OperationKey: []byte("historical"), UserID: target.Id, Quota: 1}).Error)
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(tc.typ, tc.typ)
			var version string
			versionSQL := "SELECT version()"
			if tc.name == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)
			cfg, err := common.Marshal([]map[string]any{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.credit-quota"}}})
			require.NoError(t, err)
			t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
			r := gin.New()
			r.POST("/api/getapi/users/:user_id/quota-credits", middleware.GetAPIAuth("getapi.users.credit-quota"), CreditGetAPIUserQuota)
			post := func(id int, body, token string) *httptest.ResponseRecorder {
				q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/getapi/users/%d/quota-credits", id), strings.NewReader(body))
				if token != "" {
					q.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				r.ServeHTTP(w, q)
				return w
			}
			cache := miniredis.RunT(t)
			previousRDB, previousRedisEnabled := common.RDB, common.RedisEnabled
			common.RDB = redis.NewClient(&redis.Options{Addr: cache.Addr()})
			common.RedisEnabled = true
			t.Cleanup(func() {
				_ = common.RDB.Close()
				common.RDB, common.RedisEnabled = previousRDB, previousRedisEnabled
			})
			cacheKey := fmt.Sprintf("user:%d", target.Id)
			_, err = accountstorage.Cache().GetUserCache(target.Id)
			require.NoError(t, err)
			require.Equal(t, "100", cache.HGet(cacheKey, "Quota"))
			body := `{"operationKey":"payment:1","quota":50}`
			first := post(target.Id, body, pat)
			require.Equal(t, http.StatusOK, first.Code, first.Body.String())
			assert.JSONEq(t, `{"success":true,"data":{"applied":true,"alreadyApplied":false,"quota":150}}`, first.Body.String())
			assert.Equal(t, "150", cache.HGet(cacheKey, "Quota"))
			// A replay reports the current balance, rather than the original snapshot.
			require.NoError(t, db.Model(&target).Update("quota", 145).Error)
			replay := post(target.Id, body, pat)
			require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
			assert.JSONEq(t, `{"success":true,"data":{"applied":false,"alreadyApplied":true,"quota":145}}`, replay.Body.String())
			assert.Equal(t, "150", cache.HGet(cacheKey, "Quota"))
			// Force a stale initial lookup of a committed receipt. The INSERT must
			// hit the real unique constraint, roll back, then replay in a fresh transaction.
			lookups, duplicateInserts := 0, 0
			const callback = "test:quota_credit_unique_retry"
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
				if receipt, ok := tx.Statement.Dest.(*model.GetAPIQuotaCredit); ok {
					lookups++
					if lookups == 1 {
						*receipt = model.GetAPIQuotaCredit{}
						tx.Error, tx.RowsAffected = gorm.ErrRecordNotFound, 0
					}
				}
			}))
			require.NoError(t, db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*model.GetAPIQuotaCredit); ok && tx.Error != nil {
					duplicateInserts++
				}
			}))
			t.Cleanup(func() {
				_ = db.Callback().Query().Remove(callback)
				_ = db.Callback().Create().Remove(callback)
			})
			require.NoError(t, db.Model(&target).Update("quota", common.MaxWalletQuota).Error)
			retried := post(target.Id, body, pat)
			require.NoError(t, db.Callback().Query().Remove(callback))
			require.NoError(t, db.Callback().Create().Remove(callback))
			require.Equal(t, http.StatusOK, retried.Code, retried.Body.String())
			assert.JSONEq(t, fmt.Sprintf(`{"success":true,"data":{"applied":false,"alreadyApplied":true,"quota":%d}}`, common.MaxWalletQuota), retried.Body.String())
			require.NoError(t, db.Model(&target).Update("quota", 145).Error)
			assert.Equal(t, 2, lookups)
			assert.Equal(t, 1, duplicateInserts)
			assert.Equal(t, "150", cache.HGet(cacheKey, "Quota"))
			assert.Equal(t, http.StatusConflict, post(target.Id, `{"operationKey":"payment:1","quota":51}`, pat).Code)
			assert.Equal(t, http.StatusConflict, post(other.Id, body, pat).Code)
			assert.Equal(t, http.StatusConflict, post(999999, body, pat).Code)
			assert.Equal(t, http.StatusNotFound, post(999999, `{"operationKey":"missing-user","quota":50}`, pat).Code)
			assert.Equal(t, http.StatusUnauthorized, post(target.Id, body, "").Code)
			cfg, err = common.Marshal([]map[string]any{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision"}}})
			require.NoError(t, err)
			t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
			assert.Equal(t, http.StatusForbidden, post(target.Id, body, pat).Code)
			cfg, err = common.Marshal([]map[string]any{{"integration_id": "test", "principal_user_id": principal.Id, "capabilities": []string{"getapi.users.provision", "getapi.users.credit-quota"}}})
			require.NoError(t, err)
			t.Setenv("GETAPI_INTEGRATIONS", string(cfg))
			for _, invalid := range []string{
				`{}`, `{"operationKey":"","quota":1}`, `{"operationKey":"x","quota":0}`, `{"operationKey":"x","quota":-1}`,
				`{"operationKey":"x","quota":1.5}`, `{"operationKey":"x","quota":null}`, `{"operationKey":null,"quota":1}`,
				`{"operationKey":"x","quota":18446744073709551615}`, `{"operationKey":"x","quota":1,"extra":true}`,
				`{"operationKey":"x","quota":1,"quota":2}`, fmt.Sprintf(`{"operationKey":%q,"quota":1}`, strings.Repeat("x", 201)),
			} {
				assert.Equal(t, http.StatusBadRequest, post(target.Id, invalid, pat).Code, invalid)
			}
			assert.Equal(t, http.StatusBadRequest, post(0, body, pat).Code)
			// Distinct binary identities must survive a case-insensitive MySQL collation.
			for _, key := range []string{"Key", "key", "key ", strings.Repeat("😀", 200)} {
				w := post(other.Id, fmt.Sprintf(`{"operationKey":%q,"quota":1}`, key), pat)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), `"applied":true`)
			}
			require.NoError(t, db.Model(&other).Update("quota", common.MaxWalletQuota).Error)
			overflow := post(other.Id, `{"operationKey":"overflow","quota":1}`, pat)
			assert.Equal(t, http.StatusBadRequest, overflow.Code, overflow.Body.String())
			var count int64
			require.NoError(t, db.Model(&model.GetAPIQuotaCredit{}).Where("operation_key = ?", []byte("overflow")).Count(&count).Error)
			assert.Zero(t, count)
			// Overflow must not consume the key: a corrected balance can be credited.
			require.NoError(t, db.Model(&other).Update("quota", 200).Error)
			assert.Equal(t, http.StatusOK, post(other.Id, `{"operationKey":"overflow","quota":1}`, pat).Code)
			for _, differentUsers := range []bool{false, true} {
				key := fmt.Sprintf("race:%v", differentUsers)
				raceBody := fmt.Sprintf(`{"operationKey":%q,"quota":25}`, key)
				ids := []int{target.Id, target.Id}
				if differentUsers {
					ids[1] = other.Id
				}
				responses := make([]*httptest.ResponseRecorder, 2)
				start := make(chan struct{})
				var wg sync.WaitGroup
				for i := range 2 {
					wg.Go(func() { <-start; responses[i] = post(ids[i], raceBody, pat) })
				}
				close(start)
				wg.Wait()
				applied, replayed, conflicts := 0, 0, 0
				for _, w := range responses {
					if w.Code == http.StatusConflict {
						conflicts++
						continue
					}
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					var envelope struct {
						Success bool                                        `json:"success"`
						Data    credit_getapi_quota.GetAPIQuotaCreditResult `json:"data"`
					}
					require.NoError(t, common.Unmarshal(w.Body.Bytes(), &envelope))
					require.True(t, envelope.Success)
					if envelope.Data.Applied {
						applied++
					}
					if envelope.Data.AlreadyApplied {
						replayed++
					}
				}
				assert.Equal(t, 1, applied)
				if differentUsers {
					assert.Equal(t, 1, conflicts)
				} else {
					assert.Equal(t, 1, replayed)
				}
				require.NoError(t, db.Model(&model.GetAPIQuotaCredit{}).Where("operation_key = ?", []byte(key)).Count(&count).Error)
				assert.EqualValues(t, 1, count)
			}
			var persisted model.User
			require.NoError(t, db.First(&persisted, target.Id).Error)
			// Same-user race adds exactly 25; the other race adds 25 to just one wallet.
			var otherPersisted model.User
			require.NoError(t, db.First(&otherPersisted, other.Id).Error)
			assert.Equal(t, 145+25+201+25, persisted.Quota+otherPersisted.Quota)
			// A lost successful response remains replayable after soft deletion.
			require.NoError(t, db.Delete(&target).Error)
			deletedReplay := post(target.Id, body, pat)
			require.Equal(t, http.StatusOK, deletedReplay.Code, deletedReplay.Body.String())
			assert.JSONEq(t, fmt.Sprintf(`{"success":true,"data":{"applied":false,"alreadyApplied":true,"quota":%d}}`, persisted.Quota), deletedReplay.Body.String())
			assert.Equal(t, http.StatusConflict, post(target.Id, `{"operationKey":"payment:1","quota":51}`, pat).Code)
			assert.Equal(t, http.StatusConflict, post(target.Id, `{"operationKey":"historical","quota":50}`, pat).Code)
			assert.Equal(t, http.StatusNotFound, post(target.Id, `{"operationKey":"deleted-new","quota":50}`, pat).Code)
			var afterDelete model.User
			require.NoError(t, db.Unscoped().First(&afterDelete, target.Id).Error)
			assert.Equal(t, persisted.Quota, afterDelete.Quota)
		})
	}
}

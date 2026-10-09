package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
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
	require.NoError(t, user.Update(false))
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
	_, err = model.RevokeUserAccessToken(envelope.Data.UserID)
	require.NoError(t, err)
	validated, err := model.ValidateAccessToken(oldPAT)
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
	dry, err := model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: active.Id, ExpectedUsername: active.Username})
	require.NoError(t, err)
	assert.Equal(t, "would_issue", dry.Outcome)
	assert.Nil(t, dry.AccessToken)
	issued, err := model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: active.Id, ExpectedUsername: active.Username, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, "issued", issued.Outcome)
	require.NotNil(t, issued.AccessToken)
	reuse, err := model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: active.Id, ExpectedUsername: active.Username})
	require.NoError(t, err)
	assert.Equal(t, "would_reuse", reuse.Outcome)
	assert.Nil(t, reuse.AccessToken)
	blockedResult, err := model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: blocked.Id, ExpectedUsername: blocked.Username, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, "blocked_no_pat", blockedResult.Outcome)
	assert.Nil(t, blockedResult.AccessToken)
	_, err = model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: admin.Id, ExpectedUsername: admin.Username})
	assert.ErrorIs(t, err, model.ErrGetAPITargetRoleDenied)
	_, err = model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: active.Id, ExpectedUsername: "wrong"})
	assert.ErrorIs(t, err, model.ErrGetAPIUsernameMismatch)
	_, err = model.InitializeGetAPIPAT(model.GetAPIInitializePATRequest{UserID: 999999, ExpectedUsername: "missing"})
	assert.ErrorIs(t, err, model.ErrGetAPIAccountNotFound)
}

func TestGetAPICustomCreateConcurrentUsernameConflict(t *testing.T) {
	_, _ = setupAccessTokenAudit(t)
	suffix := fmt.Sprintf("%d", common.GetTimestamp())
	req := model.GetAPICreateUserRequest{Username: "concurrent-" + suffix, Password: "safe-password", DisplayName: "Concurrent"}
	results := make([]*model.GetAPICreateCredential, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range 2 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			results[i], errs[i] = model.ProvisionGetAPIUser(common.RoleAdminUser, req)
		}(i)
	}
	close(start)
	group.Wait()
	if errs[0] == nil {
		require.Error(t, errs[1])
		assert.True(t, errors.Is(errs[1], model.ErrGetAPICreateConflict) || errors.Is(errs[1], model.ErrGetAPICredentialUnavailable))
	} else {
		require.Error(t, errs[0])
		assert.True(t, errors.Is(errs[0], model.ErrGetAPICreateConflict) || errors.Is(errs[0], model.ErrGetAPICredentialUnavailable))
		assert.NoError(t, errs[1])
	}
	var persisted int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", req.Username).Count(&persisted).Error)
	assert.Equal(t, int64(1), persisted)
	_, err := model.ProvisionGetAPIUser(common.RoleAdminUser, req)
	assert.ErrorIs(t, err, model.ErrGetAPICreateConflict)
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
						Data model.GetAPICreateCredential `json:"data"`
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
			_, err = model.ProvisionGetAPIUser(common.RoleCommonUser, model.GetAPICreateUserRequest{Username: "quota-rollback", Password: "safe-password", InitialQuota: &quota})
			require.ErrorIs(t, err, model.ErrGetAPICapabilityDenied)
			require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "quota-rollback").Count(&deniedCount).Error)
			assert.Zero(t, deniedCount)
		})
	}
}

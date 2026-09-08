package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestGetAPICustomCreateAndInitializeRetainsPAT(t *testing.T) {
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
			UserID      int    `json:"user_id"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &env))
	old := env.Data.AccessToken
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
	for _, body := range []string{`{"username":"x","password":"safe-password","display_name":"x","external_account_id":"bad"}`, `{"username":"x","password":null,"display_name":"x"}`, `{"username":"x","username":"y","password":"safe-password","display_name":"x"}`} {
		q := httptest.NewRequest(http.MethodPost, "/api/getapi/users", strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+pat)
		q.Header.Set("Idempotency-Key", "x")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	}
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

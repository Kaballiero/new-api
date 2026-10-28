package apikeys_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	routing "github.com/QuantumNous/new-api/gateway/routing"
	"github.com/QuantumNous/new-api/internal/contracttest"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Values that change on every run. Keys keep their shape after masking, so a
// masked key ("xxxx**********xxxx") and a full key ("xxx…x") stay distinguishable.
var dynamic = []string{"created_time", "accessed_time", "key", "keys", "created_at", "event_id", "request_id"}

// TestAPIKeysHTTPContract pins the /api/token responses and audit records.
// The steps share one database and run in order: later steps see earlier writes.
func TestAPIKeysHTTPContract(t *testing.T) {
	env := contracttest.New(t)
	steps := []struct {
		name, method, path, bearer, body string
	}{
		{"list_anonymous", http.MethodGet, "/api/token/", contracttest.Anonymous, ""},
		{"list_invalid_bearer", http.MethodGet, "/api/token/", "not-a-valid-token", ""},
		{"create_invalid_json", http.MethodPost, "/api/token/", env.User, `{`},
		{"create", http.MethodPost, "/api/token/", env.User, `{"name":"contract","remain_quota":1000,"expired_time":-1,"unlimited_quota":false}`},
		{"create_second_pat", http.MethodPost, "/api/token/", env.UserPAT, `{"name":"second","remain_quota":10,"expired_time":-1}`},
		{"create_name_too_long", http.MethodPost, "/api/token/", env.User, `{"name":"012345678901234567890123456789012345678901234567890","expired_time":-1}`},
		{"list", http.MethodGet, "/api/token/?p=1&size=10", env.User, ""},
		{"list_pat", http.MethodGet, "/api/token/", env.UserPAT, ""},
		{"search", http.MethodGet, "/api/token/search?keyword=contract", env.User, ""},
		{"get", http.MethodGet, "/api/token/1", env.User, ""},
		{"get_missing", http.MethodGet, "/api/token/999", env.User, ""},
		{"get_foreign", http.MethodGet, "/api/token/1", env.Admin, ""},
		{"auto_groups", http.MethodGet, "/api/token/auto-groups", env.User, ""},
		{"update", http.MethodPut, "/api/token/", env.User, `{"id":1,"name":"renamed","remain_quota":500,"expired_time":-1,"unlimited_quota":false}`},
		{"update_status_only", http.MethodPut, "/api/token/?status_only=true", env.User, `{"id":2,"status":2}`},
		{"update_foreign", http.MethodPut, "/api/token/", env.Admin, `{"id":1,"name":"stolen","expired_time":-1}`},
		{"get_after_foreign_update", http.MethodGet, "/api/token/1", env.User, ""},
		{"reveal_key", http.MethodPost, "/api/token/1/key", env.User, ""},
		{"reveal_key_pat", http.MethodPost, "/api/token/1/key", env.UserPAT, ""},
		{"reveal_key_foreign", http.MethodPost, "/api/token/1/key", env.Admin, ""},
		{"reveal_keys_batch", http.MethodPost, "/api/token/batch/keys", env.User, `{"ids":[1,2,999]}`},
		{"delete_batch_empty", http.MethodPost, "/api/token/batch", env.User, `{"ids":[]}`},
		{"delete_batch_foreign", http.MethodPost, "/api/token/batch", env.Admin, `{"ids":[1,2]}`},
		{"delete_foreign", http.MethodDelete, "/api/token/1", env.Admin, ""},
		{"delete", http.MethodDelete, "/api/token/2", env.User, ""},
		{"delete_batch", http.MethodPost, "/api/token/batch", env.User, `{"ids":[1]}`},
		{"list_after_delete", http.MethodGet, "/api/token/", env.User, ""},
	}
	for _, step := range steps {
		contracttest.Golden(t, step.name, env.Do(step.method, step.path, step.bearer, step.body), dynamic...)
	}
	contracttest.GoldenValue(t, "audit_log", env.AuditLog(t), dynamic...)
}

// TestAPIKeyUsageHTTPContract pins the API-key authenticated usage endpoint.
func TestAPIKeyUsageHTTPContract(t *testing.T) {
	env := contracttest.New(t)
	contracttest.Golden(t, "usage_create", env.Do(http.MethodPost, "/api/token/", env.User, `{"name":"usage","remain_quota":1000,"expired_time":-1}`), dynamic...)
	contracttest.Golden(t, "usage_create_disabled", env.Do(http.MethodPost, "/api/token/", env.User, `{"name":"disabled","remain_quota":1000,"expired_time":-1}`), dynamic...)
	contracttest.Golden(t, "usage_disable", env.Do(http.MethodPut, "/api/token/?status_only=true", env.User, `{"id":2,"status":2}`), dynamic...)

	reveal := func(id string) string {
		recorder := env.Do(http.MethodPost, "/api/token/"+id+"/key", env.User, "")
		var response struct {
			Data struct {
				Key string `json:"key"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		require.NotEmpty(t, response.Data.Key)
		return response.Data.Key
	}
	active, disabled := reveal("1"), reveal("2")

	contracttest.Golden(t, "usage_anonymous", env.Do(http.MethodGet, "/api/usage/token/", contracttest.Anonymous, ""), dynamic...)
	contracttest.Golden(t, "usage_invalid_key", env.Do(http.MethodGet, "/api/usage/token/", "sk-invalid", ""), dynamic...)
	contracttest.Golden(t, "usage_disabled_key", env.Do(http.MethodGet, "/api/usage/token/", "sk-"+disabled, ""), dynamic...)
	contracttest.Golden(t, "usage", env.Do(http.MethodGet, "/api/usage/token/", "sk-"+active, ""), dynamic...)
}

// TestGatewayCredentialHTTPContract records authentication without paid upstream IO.
// Context values include Go types because legacy relay consumers use typed getters.
func TestGatewayCredentialHTTPContract(t *testing.T) {
	env := contracttest.New(t)
	previousGroups := setting.UserUsableGroups2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"default","vip":"vip","auto":"auto","retired":"retired"}`))
	t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups)) })
	contextValues := func(c *gin.Context) map[string]any {
		values := map[string]any{}
		for _, key := range []string{"id", "token_id", "token_key", "token_name", "token_unlimited_quota", "token_quota", "token_model_limit_enabled", "token_model_limit", "token_group", "token_cross_group_retry", "token_auto_groups", "group", "user_group", "user_quota", "user_status", "user_email", "username", "user_setting"} {
			if value, ok := c.Get(key); ok {
				values[key] = map[string]any{"value": value, "type": reflect.TypeOf(value).String()}
			}
		}
		return values
	}
	var lastContext map[string]any
	capture := func(c *gin.Context) { c.Next(); lastContext = contextValues(c) }
	snapshot := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"context": contextValues(c), "constraints": routing.GetChannelConstraints(c)})
	}
	for _, path := range []string{"/v1/models", "/v1/messages", "/v1beta/models/gemini", "/mj/auth-contract"} {
		env.Route(http.MethodGet, path, capture, middleware.TokenAuth(), snapshot)
	}
	env.Route(http.MethodPost, "/v1/chat/completions", capture, middleware.TokenAuth(), middleware.Distribute(), snapshot)
	env.Route(http.MethodGet, "/contract/mixed", capture, middleware.TokenOrUserAuth(), snapshot)
	env.Route(http.MethodGet, "/contract/read", capture, middleware.TokenAuthReadOnly(), snapshot)
	ips := "10.0.0.1"
	tokens := []model.Token{
		{Id: 1, UserId: 1, Key: "gatewayvalid", Name: "gateway", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100},
		{Id: 2, UserId: 1, Key: "gatewaydisabled", Status: common.TokenStatusDisabled, ExpiredTime: -1, RemainQuota: 100},
		{Id: 3, UserId: 1, Key: "gatewayexpired", Status: common.TokenStatusEnabled, ExpiredTime: 1, RemainQuota: 100},
		{Id: 4, UserId: 1, Key: "gatewayexhausted", Status: common.TokenStatusEnabled, ExpiredTime: -1},
		{Id: 5, UserId: 4, Key: "gatewaybanned", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100},
		{Id: 6, UserId: 1, Key: "gatewayip", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, AllowIps: &ips},
		{Id: 7, UserId: 1, Key: "gatewaylimited", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, ModelLimitsEnabled: true, ModelLimits: "allowed,second", AutoGroups: `["vip","default"]`},
		{Id: 8, UserId: 2, Key: "gatewayadmin", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true},
		{Id: 9, UserId: 1, Key: "gatewaygroup", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, Group: "vip"},
		{Id: 10, UserId: 1, Key: "gatewayforbidden", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, Group: "forbidden"},
		{Id: 11, UserId: 1, Key: "gatewayretired", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, Group: "retired"},
		{Id: 12, UserId: 1, Key: "gatewayauto", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, Group: "auto", AutoGroups: `["vip","default"]`, CrossGroupRetry: true},
		{Id: 13, UserId: 1, Key: "gatewaymalformed", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, AutoGroups: `not-json`},
		{Id: 14, UserId: 1, Key: "gatewayemptyauto", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 100, AutoGroups: `[]`},
	}
	require.NoError(t, env.DB.Create(&model.User{Id: 4, Username: "banned", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, Group: "default", AffCode: "banned", AuthVersion: 1}).Error)
	for i := range tokens {
		require.NoError(t, env.DB.Create(&tokens[i]).Error)
	}
	type step struct{ name, path, header, value, body string }
	steps := []step{
		{"google_overrides_query", "/v1/models?key=sk-unknown", "x-goog-api-key", "sk-gatewayvalid", ""},
		{"query_overrides_anthropic", "/v1/models?key=sk-gatewayvalid", "x-api-key", "sk-unknown", ""},
		{"ws_overrides_bearer", "/v1/models", "Sec-WebSocket-Protocol", "openai-insecure-api-key.sk-gatewayvalid", ""},
		{"anthropic_overrides_ws", "/v1/models", "x-api-key", "sk-gatewayvalid", ""},
		{"midjourney_sentinel", "/mj/auth-contract", "mj-api-secret", "sk-gatewayvalid", ""},
		{"group", "/v1/models", "Authorization", "Bearer sk-gatewaygroup", ""},
		{"group_forbidden", "/v1/models", "Authorization", "Bearer sk-gatewayforbidden", ""},
		{"group_deprecated", "/v1/models", "Authorization", "Bearer sk-gatewayretired", ""},
		{"auto_groups", "/v1/models", "Authorization", "Bearer sk-gatewayauto", ""},
		{"auto_malformed", "/v1/models", "Authorization", "Bearer sk-gatewaymalformed", ""},
		{"auto_inherit", "/v1/models", "Authorization", "Bearer sk-gatewayemptyauto", ""},
		{"read_alternate_ignored", "/contract/read", "x-api-key", "sk-gatewayvalid", ""},
		{"bearer", "/v1/models", "Authorization", "Bearer sk-gatewayvalid", ""},
		{"anthropic", "/v1/messages", "x-api-key", "sk-gatewayvalid", ""},
		{"query", "/v1beta/models/gemini?key=sk-gatewayvalid", "", "", ""},
		{"google", "/v1beta/models/gemini", "x-goog-api-key", "sk-gatewayvalid", ""},
		{"websocket", "/v1/models", "Sec-WebSocket-Protocol", "realtime, openai-insecure-api-key.sk-gatewayvalid, openai-beta.realtime-v1", ""},
		{"midjourney", "/mj/auth-contract", "mj-api-secret", "sk-gatewayvalid", ""},
		{"mixed_key", "/contract/mixed", "Authorization", "Bearer sk-gatewayvalid", ""},
		{"missing", "/v1/models", "", "", ""},
		{"unknown", "/v1/models", "Authorization", "Bearer sk-unknown", ""},
		{"ip_denied", "/v1/models", "Authorization", "Bearer sk-gatewayip", ""},
		{"limited_context", "/v1/models", "Authorization", "Bearer sk-gatewaylimited", ""},
		{"model_denied", "/v1/chat/completions", "Authorization", "Bearer sk-gatewaylimited", `{"model":"forbidden"}`},
		{"pin_admin", "/v1/models", "Authorization", "Bearer sk-gatewayadmin-42", ""},
		{"pin_user", "/v1/models", "Authorization", "Bearer sk-gatewayvalid-42", ""},
		{"pin_invalid", "/v1/models", "Authorization", "Bearer sk-gatewayadmin-bad", ""},
	}
	for _, state := range []string{"disabled", "expired", "exhausted", "banned"} {
		steps = append(steps, step{state, "/v1/models", "Authorization", "Bearer sk-gateway" + state, ""}, step{"read_" + state, "/contract/read", "Authorization", "Bearer sk-gateway" + state, ""})
	}
	steps = append(steps, step{"read_valid", "/contract/read", "Authorization", "Bearer sk-gatewayvalid", ""}, step{"read_missing", "/contract/read", "", "", ""}, step{"read_unknown", "/contract/read", "Authorization", "Bearer sk-unknown", ""})
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			method := http.MethodGet
			if step.body != "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, step.path, strings.NewReader(step.body))
			req.RemoteAddr = "192.0.2.1:1234"
			switch step.name {
			case "ws_overrides_bearer":
				req.Header.Set("Authorization", "Bearer sk-unknown")
			case "anthropic_overrides_ws":
				req.Header.Set("Sec-WebSocket-Protocol", "openai-insecure-api-key.sk-unknown")
			case "midjourney_sentinel":
				req.Header.Set("Authorization", "Bearer midjourney-proxy")
			}
			req.Header.Set("Accept-Language", "en")
			if step.header != "" {
				req.Header.Set(step.header, step.value)
			}
			if step.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := env.Serve(req)
			contracttest.Golden(t, "gateway_"+step.name, rec, "request_id")
			if rec.Code >= 400 {
				contracttest.GoldenValue(t, "gateway_"+step.name+"_context", lastContext)
			}
		})
	}
	var states []model.Token
	require.NoError(t, env.DB.Order("id").Find(&states).Error)
	result := map[string]int{}
	for _, token := range states {
		result[fmt.Sprint(token.Id)] = token.Status
	}
	contracttest.GoldenValue(t, "gateway_persisted_status", result)
	for _, step := range []struct{ name, token string }{{"valid", env.UserPAT}, {"invalid", "unknownpat"}} {
		contracttest.Golden(t, "gateway_pat_"+step.name, env.Do(http.MethodGet, "/api/token/", step.token, ""), dynamic...)
	}
	require.NoError(t, env.DB.Model(&model.User{}).Where("id = ?", env.UserID).Update("access_token", nil).Error)
	contracttest.Golden(t, "gateway_pat_revoked", env.Do(http.MethodGet, "/api/token/", env.UserPAT, ""), dynamic...)
}

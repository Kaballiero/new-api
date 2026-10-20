package apikeys_test

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/contracttest"
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

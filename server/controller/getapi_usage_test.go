package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAPIUsageAuthorization(t *testing.T) {
	principal, pat := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.AutoMigrate(&model.QuotaData{}))
	router := gin.New()
	router.POST("/api/getapi/usage/aggregate", middleware.GetAPIAuth("getapi.usage.read"), GetAPIUsageAggregate)
	router.POST("/api/getapi/usage/logs", middleware.GetAPIAuth("getapi.usage.read"), GetAPIUsageLogs)
	for _, endpoint := range []struct{ path, body string }{
		{"aggregate", `{"user_ids":[1],"start_timestamp":0,"end_timestamp":10,"group_by":"model"}`},
		{"logs", `{"user_id":1,"start_timestamp":0,"end_timestamp":10,"limit":10}`},
	} {
		for _, tc := range []struct {
			name       string
			principal  int
			cap, token string
			status     int
		}{
			{"anonymous", principal.Id, "getapi.usage.read", "", http.StatusUnauthorized},
			{"missing capability", principal.Id, "getapi.users.provision", pat, http.StatusForbidden},
			{"other principal", principal.Id + 1, "getapi.usage.read", pat, http.StatusForbidden},
			{"authorized", principal.Id, "getapi.usage.read", pat, http.StatusOK},
		} {
			t.Run(endpoint.path+"/"+tc.name, func(t *testing.T) {
				config, err := common.Marshal([]map[string]any{{"integration_id": "usage-test", "principal_user_id": tc.principal, "capabilities": []string{tc.cap}}})
				require.NoError(t, err)
				t.Setenv("GETAPI_INTEGRATIONS", string(config))
				req := httptest.NewRequest(http.MethodPost, "/api/getapi/usage/"+endpoint.path, strings.NewReader(endpoint.body))
				req.Header.Set("Content-Type", "application/json")
				if tc.token != "" {
					req.Header.Set("Authorization", "Bearer "+tc.token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				assert.Equal(t, tc.status, response.Code, response.Body.String())
				assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				if tc.status == http.StatusOK {
					assert.Contains(t, response.Body.String(), `"success":true`)
				}
			})
		}
	}
}

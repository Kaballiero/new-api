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

func TestAdminLogUserQueryGuard(t *testing.T) {
	user, token := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	router := gin.New()
	router.GET("/api/log/user/:user_id", middleware.AdminAuth(), GetAdminUserLogs)
	router.POST("/api/log/query", middleware.AdminAuth(), QueryAdminLogs)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/log/user/1"}, {"POST", "/api/log/query"}} {
		for _, bearer := range []string{"", token} {
			request := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{"user_ids":[]}`))
			if bearer != "" {
				request.Header.Set("Authorization", "Bearer "+bearer)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if bearer == "" {
				assert.Equal(t, http.StatusUnauthorized, response.Code)
			} else {
				assert.Equal(t, http.StatusForbidden, response.Code)
			}
		}
	}
}

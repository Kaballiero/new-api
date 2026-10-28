package router

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// performRawPathRequest keeps the request path exactly as written. httptest
// helpers normalize or re-parse the target, which would hide the very paths
// this test covers.
func performRawPathRequest(handler http.Handler, method, rawPath string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/placeholder", strings.NewReader(""))
	request.URL = &url.URL{Path: rawPath}
	request.RequestURI = rawPath
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestWebFallbackRejectsDenormalizedAPIPaths(t *testing.T) {
	outer := gin.New()
	SetWebRouter(outer, WebAssets{IndexPage: []byte("dashboard")}, func(c *gin.Context) { c.Next() })

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "//v1/models"},
		{http.MethodGet, "///v1/models"},
		{http.MethodGet, "/./v1/models"},
		{http.MethodGet, "/v1//models"},
		{http.MethodGet, "//v1beta/models"},
		{http.MethodGet, "//api/status"},
		{http.MethodGet, "//mj/submit/imagine"},
		{http.MethodGet, "//dashboard/billing/usage"},
		{http.MethodPost, "//v1/chat/completions"},
	}
	for _, testCase := range cases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			response := performRawPathRequest(outer, testCase.method, testCase.path)
			assert.Equal(t, http.StatusNotFound, response.Code)
			assert.Contains(t, response.Body.String(), "Invalid URL")
			assert.Contains(t, response.Header().Get("Content-Type"), "application/json")
			assert.NotEqual(t, "dashboard", response.Body.String())
		})
	}
}

func TestWebFallbackStillServesDashboardShell(t *testing.T) {
	outer := gin.New()
	SetWebRouter(outer, WebAssets{IndexPage: []byte("dashboard")}, func(c *gin.Context) { c.Next() })

	for _, path := range []string{"/", "/dashboard", "/security", "/pricing", "//security"} {
		t.Run(path, func(t *testing.T) {
			response := performRawPathRequest(outer, http.MethodGet, path)
			assert.Equal(t, http.StatusOK, response.Code)
			assert.Equal(t, "dashboard", response.Body.String())
		})
	}
}

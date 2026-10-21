package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy PAT handlers: a token collision is a
// localized {success:false} with HTTP 200, everything else goes to the shared
// security operation writer unchanged.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cause := errors.New("disk full")
	cases := []struct {
		name      string
		lang      string
		err       error
		want      string
		delegated error
	}{
		{name: "duplicate", err: domain.ErrTokenDuplicate, want: `{"success":false,"message":"Please retry, the system generated a duplicate UUID!"}`},
		{name: "duplicate zh", lang: "zh-CN", err: domain.ErrTokenDuplicate, want: `{"success":false,"message":"请重试，系统生成的 UUID 竟然重复了！"}`},
		{name: "internal", err: domain.Internal(cause), delegated: cause},
		{name: "plain", err: cause, delegated: cause},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.lang != "" {
				c.Set(string(constant.ContextKeyLanguage), tc.lang)
			}
			var delegated error
			WriteError(c, tc.err, func(c *gin.Context, err error) {
				delegated = err
				c.JSON(http.StatusTeapot, gin.H{"delegated": true})
			})
			if tc.delegated != nil {
				require.ErrorIs(t, delegated, tc.delegated)
				assert.Equal(t, tc.err, delegated, "the error reaches the security writer unchanged")
				assert.Equal(t, http.StatusTeapot, recorder.Code)
				return
			}
			assert.Nil(t, delegated)
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

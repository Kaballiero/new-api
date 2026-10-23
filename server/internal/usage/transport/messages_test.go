package transport

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/usage/domain"
)

func TestUsageMessageLocalesAndDirectBoundary(t *testing.T) {
	require.NoError(t, i18n.Init())
	assert.Equal(t, "This endpoint is deprecated", Translate(nil, i18n.MsgUsageDeprecated))
	c, _ := gin.CreateTestContext(nil)
	assert.Equal(t, "Invalid token", Translate(c, i18n.MsgUsageInvalidToken))
	for _, tc := range []struct{ language, want string }{
		{"", "Failed to query logs"}, {"en", "Failed to query logs"},
		{"ru", "Не удалось получить журналы"}, {"zh-CN", "查询日志失败"}, {"zh-TW", "查询日志失败"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Request.Header.Set("Accept-Language", tc.language)
			assert.Equal(t, tc.want, Message(c, domain.ErrLogQuery))
			assert.Equal(t, "unclassified storage error", Message(c, errors.New("unclassified storage error")))
		})
	}
}

func TestMessageUsesErrorIdentityInsteadOfText(t *testing.T) {
	require.NoError(t, i18n.Init())
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.Header.Set("Accept-Language", "ru")
	assert.Equal(t, "Не удалось получить журналы", Message(c, fmt.Errorf("storage wrapper: %w", domain.ErrLogQuery)))
	assert.Equal(t, domain.ErrLogQuery.Error(), Message(c, errors.New(domain.ErrLogQuery.Error())))
	period := &domain.InvalidRankingPeriod{Period: "unsupported"}
	assert.Equal(t, "недопустимый период рейтинга: unsupported", Message(c, fmt.Errorf("wrapped: %w", period)))
	assert.Equal(t, period.Error(), Message(c, errors.New(period.Error())))
}

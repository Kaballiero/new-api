package transport_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	usage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/transport"
	billing "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchContentUsesRequestAndRecipientLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct{ language, request, rejected, logItem, logAggregate string }{
		{"", "request one: max_tokens must be in 1..100", "The provider rejected Batch creation.", "Batch request", "Batch aggregate"},
		{"en", "request one: max_tokens must be in 1..100", "The provider rejected Batch creation.", "Batch request", "Batch aggregate"},
		{"ru", "Запрос one: max_tokens должен быть в диапазоне 1..100", "Провайдер отклонил создание пакетной задачи.", "Пакетный запрос", "Пакетная задача: общий расход"},
		{"zh-CN", "request one: max_tokens must be in 1..100", "The provider rejected Batch creation.", "Batch request", "Batch aggregate"},
		{"zh-TW", "request one: max_tokens must be in 1..100", "The provider rejected Batch creation.", "Batch request", "Batch aggregate"},
		{"unknown", "request one: max_tokens must be in 1..100", "The provider rejected Batch creation.", "Batch request", "Batch aggregate"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/", nil)
			c.Set(string(constant.ContextKeyLanguage), tc.language)
			transport.Error(c, &domain.RuleError{Kind: domain.InvalidItem, ItemID: "one", Cause: &domain.RuleError{Kind: domain.OutputLimitOutOfRange, Field: "max_tokens", Limit: 100}})
			require.Equal(t, 400, recorder.Code)
			var response struct {
				Error struct{ Code, Message string }
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, "invalid_request", response.Error.Code)
			assert.Equal(t, tc.request, response.Error.Message)
			job := domain.Job{PublicID: "batch-text", Status: "failed", LastError: `{"version":1,"phase":"create","http_status":400}`}
			payload, err := common.Marshal(dto.Job(job).WithLanguage(tc.language))
			require.NoError(t, err)
			require.NoError(t, common.Unmarshal(payload, &response))
			assert.Equal(t, "batch_creation_rejected", response.Error.Code)
			assert.Equal(t, tc.rejected, response.Error.Message)
			account := domain.LogAccount{Language: tc.language}
			item, err := (usage.BatchLogs{}).Item(job, account, "one", 1, `{}`)
			require.NoError(t, err)
			var log model.Log
			require.NoError(t, common.UnmarshalJsonStr(item, &log))
			assert.Equal(t, tc.logItem, log.Content)
			aggregate, err := (usage.BatchLogs{}).Aggregate(job, account, 1, 0, billing.ProviderBasis{})
			require.NoError(t, err)
			require.NoError(t, common.UnmarshalJsonStr(aggregate, &log))
			assert.Equal(t, tc.logAggregate, log.Content)
		})
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(string(constant.ContextKeyLanguage), "ru")
	transport.Error(c, &domain.RuleError{Kind: domain.InvalidBatchPrice, Cause: errors.New("unowned shared pricing error")})
	assert.Contains(t, recorder.Body.String(), "unowned shared pricing error")
}

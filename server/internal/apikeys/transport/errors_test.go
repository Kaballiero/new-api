package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/apikeys/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy failure-kind mapping of the API key
// handlers: status, code and English message.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		err  error
		want string
		code int
	}{
		{"missing", domain.ErrKeyNotFound, `{"success":false,"code":"token_not_found","message":"Token not found"}`, http.StatusNotFound},
		{"internal", domain.Internal(errors.New("disk full")), `{"success":false,"code":"internal_error","message":"disk full"}`, http.StatusInternalServerError},
		{"key id required", domain.Internal(domain.ErrKeyIDRequired), `{"success":false,"code":"internal_error","message":"Token ID or user ID is empty"}`, http.StatusInternalServerError},
		{"search wildcards", domain.ErrSearchConsecutiveWildcards, `{"success":false,"code":"internal_error","message":"Consecutive % wildcards are not allowed in the search pattern"}`, http.StatusInternalServerError},
		{"search wildcard count", domain.ErrSearchTooManyWildcards, `{"success":false,"code":"internal_error","message":"The search pattern can contain at most 2 % wildcards"}`, http.StatusInternalServerError},
		{"search keyword", domain.ErrSearchKeywordTooShort, `{"success":false,"code":"internal_error","message":"The keyword must be at least 2 characters long for fuzzy search"}`, http.StatusInternalServerError},
		{"search count", domain.ErrSearchCountFailed, `{"success":false,"code":"internal_error","message":"Failed to count tokens"}`, http.StatusInternalServerError},
		{"fuzzy limit", domain.ErrFuzzySearchLimit, `{"success":false,"code":"internal_error","message":"Too many tokens: only exact search is allowed, do not use the % wildcard"}`, http.StatusInternalServerError},
		{"search failed", domain.ErrSearchFailed, `{"success":false,"code":"internal_error","message":"Failed to search tokens"}`, http.StatusInternalServerError},
		{"plain", errors.New("lookup failed"), `{"success":false,"message":"lookup failed"}`, http.StatusOK},
		{"usage", domain.ErrUsageUnavailable, `{"success":false,"message":"Failed to get token info, please try again later"}`, http.StatusOK},
		{"name", domain.ErrNameTooLong, `{"success":false,"code":"token_name_too_long","message":"Token name is too long"}`, http.StatusBadRequest},
		{"quota negative", domain.ErrQuotaNegative, `{"success":false,"code":"token_quota_negative","message":"Quota value cannot be negative"}`, http.StatusBadRequest},
		{"quota exceed", &domain.LimitError{Err: domain.ErrQuotaExceedMax, Max: 1000}, `{"success":false,"code":"token_quota_exceed_max","message":"Quota value exceeds valid range, maximum is 1000"}`, http.StatusBadRequest},
		{"count", &domain.LimitError{Err: domain.ErrKeyCountLimit, Max: 3}, `{"success":false,"code":"token_count_limit","message":"Maximum token count reached (3)"}`, http.StatusConflict},
		{"generate", domain.ErrKeyGenerate, `{"success":false,"code":"token_generate_failed","message":"Failed to generate token"}`, http.StatusInternalServerError},
		{"expired", domain.ErrExpiredCannotEnable, `{"success":false,"code":"token_expired_cannot_enable","message":"Token has expired and cannot be enabled. Please modify the expiration time or set it to never expire"}`, http.StatusConflict},
		{"exhausted", domain.ErrExhaustedCannotEnable, `{"success":false,"code":"token_exhausted_cannot_enable","message":"Token quota is exhausted and cannot be enabled. Please modify the remaining quota or set it to unlimited"}`, http.StatusConflict},
		{"auto too many", &domain.LimitError{Err: domain.ErrAutoGroupsTooMany, Max: 2}, `{"success":false,"message":"A token can select at most 2 Auto groups"}`, http.StatusOK},
		{"auto duplicate", &domain.GroupError{Err: domain.ErrAutoGroupDuplicate, Group: "vip"}, `{"success":false,"message":"Auto group vip is duplicated"}`, http.StatusOK},
		{"auto invalid", &domain.GroupError{Err: domain.ErrAutoGroupInvalid, Group: "vip"}, `{"success":false,"message":"Auto group vip is unavailable or unauthorized"}`, http.StatusOK},
		{"batch many", &domain.LimitError{Err: domain.ErrBatchTooLarge, Max: 100}, `{"success":false,"message":"Too many items in batch request, maximum is 100"}`, http.StatusOK},
		{"batch empty", domain.ErrBatchEmpty, `{"success":false,"message":"Invalid parameters"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			WriteError(c, tc.err)
			assert.Equal(t, tc.code, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

func TestWriteErrorLocalizesMessage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	for lang, want := range map[string]string{"ru": "Не удалось выполнить поиск токенов", "zh-CN": "搜索令牌失败"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Set(string(constant.ContextKeyLanguage), lang)
		WriteError(c, domain.ErrSearchFailed)
		assert.Equal(t, http.StatusInternalServerError, recorder.Code)
		assert.JSONEq(t, `{"success":false,"code":"internal_error","message":"`+want+`"}`, recorder.Body.String(), lang)
	}
}

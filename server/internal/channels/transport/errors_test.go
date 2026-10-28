package transport

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expectations repeat the legacy failure-kind mapping of the channel
// handlers: status, code and message. Chinese messages are checked in zh-CN,
// where they keep the previous text.
func TestWriteErrorKeepsLegacyResponses(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		lang string
		err  error
		want string
		code int
	}{
		{"missing", "en", domain.ErrChannelNotFound, `{"success":false,"code":"channel_not_found","message":"Channel does not exist"}`, http.StatusNotFound},
		{"internal", "en", domain.Internal(errors.New("disk full")), `{"success":false,"code":"internal_error","message":"disk full"}`, http.StatusInternalServerError},
		{"plain", "en", errors.New("lookup failed"), `{"success":false,"message":"lookup failed"}`, http.StatusOK},
		{"english sentinel", "en", domain.ErrTaskPluginBindDenied, `{"success":false,"message":"task plugin channels require the task_plugin.bind permission"}`, http.StatusOK},
		{"invalid id", "en", domain.ErrChannelIDInvalid, `{"success":false,"message":"invalid channel id"}`, http.StatusOK},
		{"status edit", "en", domain.ErrStatusNotEditable, `{"success":false,"message":"Invalid parameters"}`, http.StatusOK},
		{"sensitive write", "zh-CN", domain.ErrSensitiveWriteDenied, `{"success":false,"message":"无权进行此操作，权限不足"}`, http.StatusOK},
		{"validation english", "en", domain.Invalid(domain.ErrNewAPIBaseURLRequired), `{"success":false,"code":"channel_validation_failed","message":"New API channel base URL cannot be empty"}`, http.StatusBadRequest},
		{"validation region", "zh-CN", domain.Invalid(domain.ErrVertexRegionRequired), `{"success":false,"code":"channel_validation_failed","message":"部署地区不能为空"}`, http.StatusBadRequest},
		{"validation detail", "zh-CN", domain.Invalid(&domain.DetailError{Err: domain.ErrChannelSettingInvalid, Detail: "invalid channel proxy"}), `{"success":false,"code":"channel_validation_failed","message":"渠道额外设置[channel setting] 格式错误：invalid channel proxy"}`, http.StatusBadRequest},
		{"validation nested", "zh-CN", domain.Invalid(&domain.DetailError{Err: domain.ErrAppendKeysInvalid, Cause: domain.ErrVertexKeysEmpty}), `{"success":false,"code":"channel_validation_failed","message":"追加密钥解析失败: 批量添加 Vertex AI 的 keys 不能为空"}`, http.StatusBadRequest},
		{"rejected", "en", domain.Reject(errors.New("no models to validate")), `{"success":false,"code":"invalid_params","message":"no models to validate"}`, http.StatusBadRequest},
		{"mode", "zh-CN", domain.ErrAddModeUnsupported, `{"success":false,"code":"mode_unsupported","message":"不支持的添加模式"}`, http.StatusBadRequest},
		{"fetch", "zh-CN", domain.ModelFetchFailed(errors.New("upstream status: 404")), `{"success":false,"code":"fetch_models_failed","message":"获取模型列表失败: upstream status: 404"}`, http.StatusBadGateway},
		{"fetch key", "zh-CN", domain.ModelFetchFailed(&domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: errors.New("no enabled keys")}), `{"success":false,"code":"fetch_models_failed","message":"获取模型列表失败: 获取渠道密钥失败: no enabled keys"}`, http.StatusBadGateway},
		{"fetch key en", "en", domain.ModelFetchFailed(&domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: errors.New("no enabled keys")}), `{"success":false,"code":"fetch_models_failed","message":"Failed to fetch model list: Failed to get channel key: no enabled keys"}`, http.StatusBadGateway},
		{"apply key", "zh-CN", &domain.DetailError{Err: domain.ErrChannelKeyUnavailable, Cause: errors.New("no enabled keys")}, `{"success":false,"message":"获取渠道密钥失败: no enabled keys"}`, http.StatusOK},
		{"tag", "zh-CN", domain.ErrTagRequired, `{"success":false,"message":"tag不能为空"}`, http.StatusOK},
		{"list", "zh-CN", domain.ErrCountTypesFailed, `{"success":false,"message":"获取渠道类型统计失败，请稍后重试"}`, http.StatusOK},
		{"multi key missing", "zh-CN", domain.ErrMultiKeyChannelNotFound, `{"success":false,"message":"渠道不存在"}`, http.StatusOK},
		{"multi key range", "zh-CN", domain.ErrKeyIndexOutOfRange, `{"success":false,"message":"密钥索引超出范围"}`, http.StatusOK},
		{"balance", "zh-CN", domain.ErrBalanceNotImplemented, `{"success":false,"message":"尚未实现"}`, http.StatusOK},
		{"codex", "zh-CN", domain.ErrCodexUsageFailed, `{"success":false,"message":"获取用量信息失败，请稍后重试"}`, http.StatusOK},
		{"copy", "zh-CN", domain.ErrCopyFailed, `{"success":false,"message":"复制渠道失败，请稍后重试"}`, http.StatusOK},
		{"ollama input", "en", domain.ErrOllamaInputRequired, `{"success":false,"message":"Channel ID and model name are required"}`, http.StatusBadRequest},
		{"ollama missing", "en", domain.ErrOllamaNotFound, `{"success":false,"message":"Channel not found"}`, http.StatusNotFound},
		{"ollama pull", "en", fmt.Errorf("%w: %s", domain.ErrOllamaPullFailed, "x"), `{"success":false,"message":"Failed to pull model: x"}`, http.StatusInternalServerError},
		{"ollama version", "zh-CN", domain.OllamaVersionFailed(errors.New("timeout")), `{"success":false,"message":"获取Ollama版本失败: timeout"}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			c.Request.Header.Set("Accept-Language", tc.lang)
			WriteError(c, tc.err)
			assert.Equal(t, tc.code, recorder.Code)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

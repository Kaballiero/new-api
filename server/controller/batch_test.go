package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	batchmath "github.com/QuantumNous/new-api/internal/adapter/billing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/fxseed"
	"github.com/QuantumNous/new-api/model"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/batch"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBatchAdmissionFreezesFinalEnvelopeProvider(t *testing.T) {
	// FX publications are process-global and intentionally have no reset API.
	// Keep this admission fixture isolated from tests of an unpublished cache.
	if os.Getenv("PROVIDER_ROUTING_ADMISSION_CHILD") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestBatchAdmissionFreezesFinalEnvelopeProvider$", "-test.timeout=60s")
		command.Env = append(os.Environ(), "PROVIDER_ROUTING_ADMISSION_CHILD=1")
		output, err := command.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	db := openChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.ModelCostFXRow{}))
	previousUpdateTask := constant.UpdateTask
	constant.UpdateTask = true
	previousQuotaUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	settings := operation_setting.GetGeneralSetting()
	previousExchangeRate := settings.CustomCurrencyExchangeRate
	settings.CustomCurrencyExchangeRate = 100
	t.Cleanup(func() {
		constant.UpdateTask = previousUpdateTask
		common.QuotaPerUnit = previousQuotaUnit
		settings.CustomCurrencyExchangeRate = previousExchangeRate
	})
	fxseed.SeedNativeRouteFX(t, db)
	client := httpclient.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	previousTransport := client.Transport
	t.Cleanup(func() { client.Transport = previousTransport })
	client.Transport = pricingCatalogTestTransport(func(request *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, request.Method, "admission must never submit paid Batch requests")
		assert.Equal(t, "https://batch.fixture/api/v1/models/test/mapped:batch/endpoints", request.URL.String())
		body := `{"data":{"id":"test/mapped:batch","endpoints":[{"tag":"cheap","max_completion_tokens":10,"pricing":{"prompt":"0","completion":"0.001"}},{"tag":"expensive","max_completion_tokens":10,"pricing":{"prompt":"0","completion":"0.003"}}]}}`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	baseURL := "https://batch.fixture/api"
	channel := model.Channel{Type: constant.ChannelTypeOpenRouter, Key: "fixture-key", BaseURL: &baseURL}
	require.NoError(t, db.Create(&channel).Error)
	user := model.User{Username: "batch-provider", Password: "unused", Quota: 1000000, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "batch-provider", RemainQuota: 1000000}
	require.NoError(t, db.Create(&token).Error)
	for _, tc := range []struct {
		name, routing, override, wantProvider string
		wantQuota                             int64
	}{
		{"no provider", "", `{}`, "", 15000},
		{"client provider", `,"provider":{"only":["cheap"],"allow_fallbacks":false}`, `{}`, `{"only":["cheap"],"allow_fallbacks":false}`, 5000},
		{"admin selects expensive", `,"provider":{"only":["cheap"]}`, `{"operations":[{"mode":"set","path":"provider","value":{"only":["expensive"],"allow_fallbacks":false},"conditions":[{"path":"endpoint","mode":"full","value":"/v1/chat/completions"}]}]}`, `{"only":["expensive"],"allow_fallbacks":false}`, 15000},
		{"legacy admin move", `,"extra_body":{"provider":{"only":["cheap"],"allow_fallbacks":false}}`, `{"operations":[{"mode":"delete","path":"provider","conditions":[{"path":"endpoint","mode":"full","value":"/v1/chat/completions"}]},{"mode":"move","from":"extra_body.provider","to":"provider","conditions":[{"path":"endpoint","mode":"full","value":"/v1/chat/completions"}]}]}`, `{"only":["cheap"],"allow_fallbacks":false}`, 5000},
		{"legacy admin replacement", `,"extra_body":{"provider":{"only":["cheap"]}}`, `{"operations":[{"mode":"set","path":"provider","value":{"only":["expensive"]},"conditions":[{"path":"endpoint","mode":"full","value":"/v1/chat/completions"}]}]}`, `{"only":["expensive"]}`, 15000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			body := `{"model":"test/model","endpoint":"/v1/chat/completions"` + tc.routing + `,"requests":[{"custom_id":"one","body":{"messages":[],"max_tokens":10,"provider":{"only":["item-provider"]}}}]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/batches", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("id", user.Id)
			common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "test/model")
			common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenRouter)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, channel.Key)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			c.Set("model_mapping", `{"test/model":"test/mapped"}`)
			var overrides map[string]any
			require.NoError(t, common.UnmarshalJsonStr(tc.override, &overrides))
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, overrides)
			PrepareBatchRequest(c)
			require.False(t, c.IsAborted(), writer.Body.String())
			CreateBatch(c)
			require.Equal(t, http.StatusAccepted, writer.Code, writer.Body.String())
			var job model.BatchJob
			require.NoError(t, db.Where("public_id = ?", c.GetString("batch_public_id")).First(&job).Error)
			var snapshot batchmath.BatchBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(job.BillingSnapshot, &snapshot))
			if tc.wantProvider == "" {
				assert.Empty(t, snapshot.Provider)
			} else {
				assert.JSONEq(t, tc.wantProvider, string(snapshot.Provider))
			}
			assert.Equal(t, tc.wantQuota, job.ReservedQuota)
			assert.InDelta(t, float64(tc.wantQuota)/500000, snapshot.OpenRouterCost.EstimatedUSD, 1e-12)
			var item model.BatchItem
			require.NoError(t, db.Where("batch_id = ?", job.ID).First(&item).Error)
			assert.JSONEq(t, `{"messages":[],"max_tokens":10,"provider":{"only":["item-provider"]}}`, item.UpstreamBody, "envelope preferences must not substitute item preferences")
			assert.Equal(t, tc.wantQuota, item.ReservedQuota)
			wire, err := (batch.OpenRouter{}).Create(batch.Submission{Model: job.UpstreamModel, Endpoint: job.Endpoint, Provider: snapshot.Provider, Items: []batch.Item{{CustomID: item.ProviderCustomID, Body: []byte(item.UpstreamBody)}}})
			require.NoError(t, err)
			var payload domain.CreateRequest
			require.NoError(t, common.Unmarshal(wire.Body, &payload))
			assert.Equal(t, string(snapshot.Provider), string(payload.Provider), "admission snapshot must be the native envelope routing")
			require.Len(t, payload.Requests, 1)
			assert.JSONEq(t, item.UpstreamBody, string(payload.Requests[0].Body))
		})
	}
}

func TestBatchEnvelopeProviderIdempotency(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.BatchJob{}))
	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = oldDB })
	var originalHash string
	for _, tc := range []struct {
		name, routing string
		status        int
	}{
		{"first", `"provider":{"only":["openai"],"allow_fallbacks":false}`, 200},
		{"same", `"provider":{"only":["openai"],"allow_fallbacks":false}`, 200},
		{"nested same", `"extra_body":{"provider":{"only":["openai"],"allow_fallbacks":false}}`, 200},
		{"different provider", `"provider":{"only":["azure"],"allow_fallbacks":false}`, 409},
		{"different fallback", `"provider":{"only":["openai"],"allow_fallbacks":true}`, 409},
		{"unsupported order", `"provider":{"order":["openai"]}`, 400},
		{"conflicting nested", `"provider":{"only":["openai"]},"extra_body":{"provider":{"only":["azure"]}}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			body := `{"model":"test","endpoint":"/v1/chat/completions",` + tc.routing + `,"requests":[{"custom_id":"one","body":{"messages":[],"max_tokens":8}}]}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/batches", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Idempotency-Key", "provider-idempotency")
			c.Set("id", 123)
			PrepareBatchRequest(c)
			require.Equal(t, tc.status, writer.Code, writer.Body.String())
			if tc.name == "first" {
				originalHash = c.GetString("batch_request_hash")
				require.NotEmpty(t, originalHash)
				job := model.BatchJob{PublicID: "batch-existing", UserID: 123, IdempotencyKey: c.GetString("batch_key_hash"), RequestHash: originalHash}
				require.NoError(t, db.Create(&job).Error)
			} else if tc.status == 200 {
				assert.Contains(t, writer.Body.String(), "batch-existing")
			}
		})
	}
}

func TestBatchGoogleOutputBoundary(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"generationConfig":{"maxOutputTokens":8,"candidateCount":1}}`, true},
		{`{"generation_config":{"max_output_tokens":8,"candidate_count":999}}`, false},
		{`{"max_tokens":8}`, false},
		{`{"generationConfig":{"maxOutputTokens":8,"candidate_count":999}}`, false},
		{`{"generationConfig":{"maxOutputTokens":18446744073709551615}}`, false},
	} {
		output, err := (gateway.Validation{}).Item([]byte(tc.body), "gemini-test", "/v1beta/generateContent")
		if tc.valid {
			require.NoError(t, err)
			assert.Equal(t, 8, output)
		} else {
			require.Error(t, err)
		}
	}
}

func TestBatchOutputLimitUsesNativeEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpoint, body string
		valid          bool
	}{
		{"/v1/chat/completions", `{"max_tokens":8}`, true},
		{"/v1/chat/completions", `{"max_output_tokens":8}`, false},
		{"/v1/responses", `{"max_output_tokens":8}`, true},
		{"/v1/responses", `{"max_tokens":8}`, false},
		{"/v1/messages", `{"max_tokens":8}`, true},
		{"/v1/messages", `{"max_completion_tokens":8}`, false},
	} {
		_, err := (gateway.Validation{}).Item([]byte(tc.body), "test", tc.endpoint)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

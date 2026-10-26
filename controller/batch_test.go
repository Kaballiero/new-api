package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/batch"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
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
	seedNativeRouteFX(t, db)
	client := service.GetHttpClient()
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
			var snapshot service.BatchBillingSnapshot
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
			var payload BatchCreateRequest
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
		output, err := validateBatchItem([]byte(tc.body), "gemini-test", "/v1beta/generateContent")
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
		_, err := validateBatchItem([]byte(tc.body), "test", tc.endpoint)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestReleaseUnsubmittedBatchEvidence(t *testing.T) {
	db := openChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.BatchLogEvent{}, &model.Log{}, &model.AuditLog{}))
	channel := model.Channel{Type: constant.ChannelTypeOpenRouter, Key: "test-key"}
	require.NoError(t, db.Create(&channel).Error)
	root := model.User{Username: "root-operator", Password: "unused", AffCode: "root-aff", Role: common.RoleRootUser, AuthVersion: 1}
	require.NoError(t, db.Create(&root).Error)
	user := model.User{Username: "release-user", Password: "unused", AffCode: "user-aff", Quota: 1000, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "release-token", RemainQuota: 1000}
	require.NoError(t, db.Create(&token).Error)
	job := model.BatchJob{PublicID: "batch_release", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, ChannelType: channel.Type, Model: "test-model", UpstreamModel: "test-model", Endpoint: "/v1/chat/completions", Status: "validating", BillingStatus: "reserved", SubmitState: "queued", BillingSnapshot: `{}`, RequestCount: 1, ReservedQuota: 100}
	require.NoError(t, model.CreateBatchReservation(&job, []model.BatchItem{{CustomID: "client-id", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 100}}))
	require.NoError(t, db.Model(&job).Updates(map[string]any{"submit_state": "submission_unknown", "billing_status": "reconciliation_required"}).Error)
	router := gin.New()
	router.POST("/api/batches/:id/release-unsubmitted", func(c *gin.Context) {
		c.Set("id", root.Id)
		c.Set("role", common.RoleRootUser)
		c.Set("username", root.Username)
	}, ReleaseUnsubmittedBatch)
	release := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/batches/batch_release/release-unsubmitted", strings.NewReader(body)))
		return recorder
	}

	for _, body := range []string{`{"evidence":"   "}`, `{}`, `{"evidence":"` + strings.Repeat("я", 4097) + `"}`} {
		recorder := release(body)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Contains(t, recorder.Body.String(), "invalid_request")
	}
	var audits int64
	require.NoError(t, db.Model(&model.AuditLog{}).Count(&audits).Error)
	assert.Zero(t, audits, "rejected input never reaches the Batch")

	evidence := strings.Repeat("ж", 3000)
	recorder := release(`{"evidence":"` + evidence + `"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.JSONEq(t, `{"success":true,"id":"batch_release","released_quota":100}`, recorder.Body.String())
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.BatchHeldQuota)
	require.NoError(t, db.First(&job, job.ID).Error)
	assert.NotContains(t, job.LastError, "жжж", "evidence stays out of Batch columns")
	assert.Empty(t, job.LeaseOwner, "the release lease is returned")

	var entry model.AuditLog
	require.NoError(t, db.Where("action = ?", "batch.release_unsubmitted").First(&entry).Error)
	assert.Equal(t, root.Id, entry.UserId)
	assert.Equal(t, model.AuditCategoryOperation, entry.Category)
	assert.True(t, entry.Success)
	assert.Equal(t, "Released the hold of unsubmitted Batch batch_release", entry.Content)
	var stored string
	require.NoError(t, common.Unmarshal(entry.Other.RootInfo["evidence"].(json.RawMessage), &stored))
	assert.Equal(t, evidence, stored)
	assert.Contains(t, string(entry.Other.RootInfo["released_quota"].(json.RawMessage)), "100")

	recorder = release(`{"evidence":"repeat"}`)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"success":true,"id":"batch_release","already_released":true}`, recorder.Body.String())

	require.NoError(t, db.Model(&job).Updates(map[string]any{"submit_state": "submitted", "upstream_id": "native-1", "last_error": ""}).Error)
	recorder = release(`{"evidence":"provider says absent"}`)
	assert.Equal(t, http.StatusConflict, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "release_ineligible")
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ?", "batch.release_unsubmitted").Count(&audits).Error)
	assert.EqualValues(t, 3, audits, "every attempt on an existing Batch is audited once")
	var rejected model.AuditLog
	require.NoError(t, db.Where("action = ? AND success = ?", "batch.release_unsubmitted", false).First(&rejected).Error)
	assert.Equal(t, http.StatusConflict, rejected.Status)
	require.NoError(t, common.Unmarshal(rejected.Other.RootInfo["evidence"].(json.RawMessage), &stored))
	assert.Equal(t, "provider says absent", stored, "rejected attempts keep their evidence too")

	const hook = "batch_test_claim_unavailable"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
		if values, ok := tx.Statement.Dest.(map[string]any); ok && strings.HasPrefix(fmt.Sprint(values["lease_owner"]), "batch-release-") {
			tx.AddError(errors.New("database unavailable"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(hook) })
	recorder = release(`{"evidence":"storage down"}`)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, "a storage failure is not a busy Batch")
	assert.Contains(t, recorder.Body.String(), "storage_unavailable")
	var unavailable model.AuditLog
	require.NoError(t, db.Where("action = ? AND status = ?", "batch.release_unsubmitted", http.StatusServiceUnavailable).First(&unavailable).Error)
	assert.False(t, unavailable.Success)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/batches/batch_missing/release-unsubmitted", strings.NewReader(`{"evidence":"none"}`)))
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	const lookupHook = "batch_test_lookup_unavailable"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(lookupHook, func(tx *gorm.DB) {
		if tx.Statement.Table == "batch_jobs" {
			tx.AddError(errors.New("database unavailable"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(lookupHook) })
	recorder = release(`{"evidence":"lookup down"}`)
	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, "a lookup failure is not a missing Batch")
	assert.Contains(t, recorder.Body.String(), "storage_unavailable")
	require.NoError(t, db.Callback().Query().Remove(lookupHook))
	var unavailableCount int64
	require.NoError(t, db.Model(&model.AuditLog{}).Where("action = ? AND status = ?", "batch.release_unsubmitted", http.StatusServiceUnavailable).Count(&unavailableCount).Error)
	assert.EqualValues(t, 2, unavailableCount, "lookup failures are audited; a missing Batch is not")
}

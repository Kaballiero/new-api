package controller

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const (
	geminiTTSUserId     = 91530
	geminiTTSTokenId    = 91531
	geminiTTSChannelId  = 91532
	geminiTTSTokenKey   = "gemini-tts-controller-key"
	geminiTTSUserQuota  = 1_000_000
	geminiTTSFlashModel = "gemini-2.5-flash-preview-tts"
	geminiTTSProModel   = "gemini-2.5-pro-preview-tts"
	geminiTTSAudioMime  = "audio/L16;codec=pcm;rate=24000"
)

var geminiTTSPCM = []byte{0x01, 0x00, 0xff, 0x7f, 0x00, 0x80}

var geminiTTSDisposableSchema = []string{
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		quota INTEGER DEFAULT 0,
		used_quota INTEGER DEFAULT 0,
		request_count INTEGER DEFAULT 0,
		setting TEXT,
		deleted_at DATETIME
	)`,
	"CREATE TABLE tokens (" +
		"id INTEGER PRIMARY KEY," +
		"user_id INTEGER," +
		"`key` TEXT," +
		"status INTEGER DEFAULT 1," +
		"name TEXT DEFAULT ''," +
		"created_time INTEGER DEFAULT 0," +
		"accessed_time INTEGER DEFAULT 0," +
		"expired_time INTEGER DEFAULT -1," +
		"remain_quota INTEGER DEFAULT 0," +
		"unlimited_quota NUMERIC DEFAULT 0," +
		"model_limits_enabled NUMERIC DEFAULT 0," +
		"model_limits TEXT DEFAULT ''," +
		"allow_ips TEXT DEFAULT ''," +
		"used_quota INTEGER DEFAULT 0," +
		"`group` TEXT DEFAULT ''," +
		"cross_group_retry NUMERIC DEFAULT 0," +
		"deleted_at DATETIME)",
	`CREATE TABLE channels (
		id INTEGER PRIMARY KEY,
		used_quota INTEGER DEFAULT 0
	)`,
	`CREATE TABLE user_subscriptions (
		id INTEGER PRIMARY KEY,
		user_id INTEGER,
		status TEXT DEFAULT '',
		end_time INTEGER DEFAULT 0
	)`,
	"CREATE TABLE logs (" +
		"id INTEGER PRIMARY KEY AUTOINCREMENT," +
		"user_id INTEGER," +
		"created_at INTEGER DEFAULT 0," +
		"type INTEGER DEFAULT 0," +
		"content TEXT DEFAULT ''," +
		"username TEXT DEFAULT ''," +
		"token_name TEXT DEFAULT ''," +
		"model_name TEXT DEFAULT ''," +
		"quota INTEGER DEFAULT 0," +
		"prompt_tokens INTEGER DEFAULT 0," +
		"completion_tokens INTEGER DEFAULT 0," +
		"use_time INTEGER DEFAULT 0," +
		"is_stream NUMERIC DEFAULT 0," +
		"channel_id INTEGER DEFAULT 0," +
		"token_id INTEGER DEFAULT 0," +
		"`group` TEXT DEFAULT ''," +
		"ip TEXT DEFAULT ''," +
		"request_id TEXT DEFAULT ''," +
		"upstream_request_id TEXT DEFAULT ''," +
		"other TEXT DEFAULT '')",
}

func setupGeminiTTSControllerFixture(t *testing.T, retryTimes int) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousSQLitePath := common.SQLitePath
	previousIsMasterNode := common.IsMasterNode
	previousRetryTimes := common.RetryTimes
	previousRedisEnabled := common.RedisEnabled
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	previousLogConsumeEnabled := common.LogConsumeEnabled
	previousMaxFileDownloadMB := constant.MaxFileDownloadMB

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SQLitePath = previousSQLitePath
		common.IsMasterNode = previousIsMasterNode
		common.RetryTimes = previousRetryTimes
		common.RedisEnabled = previousRedisEnabled
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
		common.LogConsumeEnabled = previousLogConsumeEnabled
		constant.MaxFileDownloadMB = previousMaxFileDownloadMB
	})

	t.Setenv("SQL_DSN", "local")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared"
	common.IsMasterNode = false
	require.NoError(t, model.InitDB())
	common.SQLitePath = previousSQLitePath
	common.IsMasterNode = previousIsMasterNode

	db := model.DB
	model.LOG_DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	common.RetryTimes = retryTimes
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	constant.MaxFileDownloadMB = 1

	for _, ddl := range geminiTTSDisposableSchema {
		require.NoError(t, db.Exec(ddl).Error)
	}

	require.NoError(t, db.Exec(
		`INSERT INTO users (id, quota, used_quota, request_count, setting) VALUES (?, ?, 0, 0, '')`,
		geminiTTSUserId, geminiTTSUserQuota).Error)
	require.NoError(t, db.Exec(
		"INSERT INTO tokens (id, user_id, `key`, status, name, remain_quota, used_quota, `group`) VALUES (?, ?, ?, 1, 'gemini-tts', ?, 0, '')",
		geminiTTSTokenId, geminiTTSUserId, geminiTTSTokenKey, geminiTTSUserQuota).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO channels (id, used_quota) VALUES (?, 0)`, geminiTTSChannelId).Error)

	applyGeminiTTSRatios(t)

	return db
}

func applyGeminiTTSRatios(t *testing.T) {
	t.Helper()

	previousModelRatio, err := common.Marshal(ratio_setting.GetModelRatioCopy())
	require.NoError(t, err)
	previousAudioCompletionRatio, err := common.Marshal(ratio_setting.GetAudioCompletionRatioCopy())
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(previousModelRatio)))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(string(previousAudioCompletionRatio)))
	})

	modelRatio := ratio_setting.GetModelRatioCopy()
	modelRatio[geminiTTSFlashModel] = 0.25
	modelRatio[geminiTTSProModel] = 0.5
	audioCompletionRatio := ratio_setting.GetAudioCompletionRatioCopy()
	audioCompletionRatio[geminiTTSFlashModel] = 20
	audioCompletionRatio[geminiTTSProModel] = 20

	updatedModelRatio, err := common.Marshal(modelRatio)
	require.NoError(t, err)
	updatedAudioCompletionRatio, err := common.Marshal(audioCompletionRatio)
	require.NoError(t, err)

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(updatedModelRatio)))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(string(updatedAudioCompletionRatio)))
}

func geminiTTSScalar(t *testing.T, db *gorm.DB, query string, args ...any) int {
	t.Helper()
	var value int
	require.NoError(t, db.Raw(query, args...).Scan(&value).Error)
	return value
}

func geminiTTSUserQuotaNow(t *testing.T, db *gorm.DB) int {
	t.Helper()
	return geminiTTSScalar(t, db, `SELECT quota FROM users WHERE id = ?`, geminiTTSUserId)
}

func geminiTTSConsumeLogCount(t *testing.T, db *gorm.DB) int {
	t.Helper()
	return geminiTTSScalar(t, db, `SELECT count(*) FROM logs WHERE type = ?`, model.LogTypeConsume)
}

func geminiTTSWaitForFullRefund(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.Eventually(t, func() bool {
		userQuota, tokenRemain, tokenUsed, err := geminiTTSInFlightBilling(db)
		return err == nil &&
			userQuota == geminiTTSUserQuota &&
			tokenRemain == geminiTTSUserQuota &&
			tokenUsed == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func geminiTTSAudioResponseBody(t *testing.T, usage dto.GeminiUsageMetadata) []byte {
	t.Helper()
	body, err := common.Marshal(dto.GeminiChatResponse{
		Candidates: []dto.GeminiChatCandidate{
			{
				Content: dto.GeminiChatContent{
					Role: "model",
					Parts: []dto.GeminiPart{
						{InlineData: &dto.GeminiInlineData{
							MimeType: geminiTTSAudioMime,
							Data:     base64.StdEncoding.EncodeToString(geminiTTSPCM),
						}},
					},
				},
			},
		},
		UsageMetadata: usage,
	})
	require.NoError(t, err)
	return body
}

func geminiTTSFullUsageMetadata() dto.GeminiUsageMetadata {
	return dto.GeminiUsageMetadata{
		PromptTokenCount:        100,
		CandidatesTokenCount:    200,
		TotalTokenCount:         300,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	}
}

type geminiTTSControllerRun struct {
	recorder            *httptest.ResponseRecorder
	upstreamCalls       int
	channelTries        int
	inFlightUserQuota   int
	inFlightTokenRemain int
	inFlightTokenUsed   int
	inFlightErr         error
}

func geminiTTSInFlightBilling(db *gorm.DB) (userQuota int, tokenRemain int, tokenUsed int, err error) {
	if err = db.Raw(`SELECT quota FROM users WHERE id = ?`, geminiTTSUserId).Scan(&userQuota).Error; err != nil {
		return
	}
	if err = db.Raw(`SELECT remain_quota FROM tokens WHERE id = ?`, geminiTTSTokenId).Scan(&tokenRemain).Error; err != nil {
		return
	}
	err = db.Raw(`SELECT used_quota FROM tokens WHERE id = ?`, geminiTTSTokenId).Scan(&tokenUsed).Error
	return
}

func runGeminiTTSControllerRelay(t *testing.T, db *gorm.DB, request dto.AudioRequest, upstream http.HandlerFunc) *geminiTTSControllerRun {
	t.Helper()

	run := &geminiTTSControllerRun{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userQuota, tokenRemain, tokenUsed, err := geminiTTSInFlightBilling(db)
		mu.Lock()
		run.upstreamCalls++
		run.inFlightUserQuota = userQuota
		run.inFlightTokenRemain = tokenRemain
		run.inFlightTokenUsed = tokenUsed
		run.inFlightErr = err
		mu.Unlock()
		upstream(w, r)
	}))
	t.Cleanup(server.Close)

	run.recorder = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(run.recorder)

	body, err := common.Marshal(request)
	require.NoError(t, err)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	common.SetContextKey(c, constant.ContextKeyUserId, geminiTTSUserId)
	common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(c, constant.ContextKeyUserQuota, geminiTTSUserQuota)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, request.Model)
	common.SetContextKey(c, constant.ContextKeyTokenId, geminiTTSTokenId)
	common.SetContextKey(c, constant.ContextKeyTokenKey, geminiTTSTokenKey)
	common.SetContextKey(c, constant.ContextKeyChannelId, geminiTTSChannelId)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-gemini-key")
	c.Set("channel_name", "gemini-tts-channel")
	c.Set("auto_ban", false)

	Relay(c, types.RelayFormatOpenAIAudio)

	require.Eventually(t, func() bool {
		return gopool.WorkerCount() == 0
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	run.channelTries = len(c.GetStringSlice("use_channel"))
	mu.Unlock()
	return run
}

func TestRelayGeminiTTSSettlesAudioQuotaThroughControllerBilling(t *testing.T) {
	cases := []struct {
		model     string
		wantQuota int
	}{
		{model: geminiTTSFlashModel, wantQuota: 1025},
		{model: geminiTTSProModel, wantQuota: 2050},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			db := setupGeminiTTSControllerFixture(t, 0)
			upstreamBody := geminiTTSAudioResponseBody(t, geminiTTSFullUsageMetadata())

			run := runGeminiTTSControllerRelay(t, db, dto.AudioRequest{
				Model: tc.model, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
			}, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(upstreamBody)
			})

			require.Equal(t, http.StatusOK, run.recorder.Code)
			assert.Equal(t, 1, run.upstreamCalls)
			assert.Equal(t, "audio/wav", run.recorder.Header().Get("Content-Type"))
			require.Len(t, run.recorder.Body.Bytes(), 44+len(geminiTTSPCM))
			assert.Equal(t, geminiTTSPCM, run.recorder.Body.Bytes()[44:])

			assert.Equal(t, geminiTTSUserQuota-tc.wantQuota, geminiTTSUserQuotaNow(t, db))
			assert.Equal(t, tc.wantQuota, geminiTTSScalar(t, db, `SELECT used_quota FROM users WHERE id = ?`, geminiTTSUserId))
			assert.Equal(t, tc.wantQuota, geminiTTSScalar(t, db, `SELECT used_quota FROM channels WHERE id = ?`, geminiTTSChannelId))
			assert.Equal(t, geminiTTSUserQuota-tc.wantQuota, geminiTTSScalar(t, db, `SELECT remain_quota FROM tokens WHERE id = ?`, geminiTTSTokenId))

			require.Equal(t, 1, geminiTTSConsumeLogCount(t, db))
			assert.Equal(t, tc.wantQuota, geminiTTSScalar(t, db, `SELECT quota FROM logs WHERE type = ?`, model.LogTypeConsume))
			assert.Equal(t, 200, geminiTTSScalar(t, db, `SELECT completion_tokens FROM logs WHERE type = ?`, model.LogTypeConsume))
		})
	}
}

func TestRelayGeminiTTSSettlesAudioQuotaWhenUpstreamReportsOnlyModalityDetails(t *testing.T) {
	previousCountToken := constant.CountToken
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
	})
	constant.CountToken = false

	db := setupGeminiTTSControllerFixture(t, 0)
	upstreamBody := geminiTTSAudioResponseBody(t, dto.GeminiUsageMetadata{
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 100}},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	})

	run := runGeminiTTSControllerRelay(t, db, dto.AudioRequest{
		Model: geminiTTSFlashModel, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamBody)
	})

	require.Equal(t, http.StatusOK, run.recorder.Code)

	const wantQuota = 1025
	assert.Equal(t, geminiTTSUserQuota-wantQuota, geminiTTSUserQuotaNow(t, db))
	assert.Equal(t, wantQuota, geminiTTSScalar(t, db, `SELECT used_quota FROM users WHERE id = ?`, geminiTTSUserId))
	assert.Equal(t, wantQuota, geminiTTSScalar(t, db, `SELECT used_quota FROM channels WHERE id = ?`, geminiTTSChannelId))
	assert.Equal(t, geminiTTSUserQuota-wantQuota, geminiTTSScalar(t, db, `SELECT remain_quota FROM tokens WHERE id = ?`, geminiTTSTokenId))
	assert.Equal(t, wantQuota, geminiTTSScalar(t, db, `SELECT used_quota FROM tokens WHERE id = ?`, geminiTTSTokenId))

	require.Equal(t, 1, geminiTTSConsumeLogCount(t, db))
	assert.Equal(t, wantQuota, geminiTTSScalar(t, db, `SELECT quota FROM logs WHERE type = ?`, model.LogTypeConsume))
	assert.Equal(t, 100, geminiTTSScalar(t, db, `SELECT prompt_tokens FROM logs WHERE type = ?`, model.LogTypeConsume))
	assert.Equal(t, 200, geminiTTSScalar(t, db, `SELECT completion_tokens FROM logs WHERE type = ?`, model.LogTypeConsume))
}

func TestRelayGeminiTTSMarksLocalPromptEstimateInConsumeLog(t *testing.T) {
	previousCountToken := constant.CountToken
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
	})
	constant.CountToken = true

	db := setupGeminiTTSControllerFixture(t, 0)
	upstreamBody := geminiTTSAudioResponseBody(t, dto.GeminiUsageMetadata{
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{{Modality: "AUDIO", TokenCount: 200}},
	})

	run := runGeminiTTSControllerRelay(t, db, dto.AudioRequest{
		Model: geminiTTSFlashModel, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
	}, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(upstreamBody)
	})

	require.Equal(t, http.StatusOK, run.recorder.Code)
	require.Equal(t, 1, geminiTTSConsumeLogCount(t, db))

	var promptTokens int
	require.NoError(t, db.Raw(`SELECT prompt_tokens FROM logs WHERE type = ?`, model.LogTypeConsume).Scan(&promptTokens).Error)
	assert.Positive(t, promptTokens)

	var other string
	require.NoError(t, db.Raw(`SELECT other FROM logs WHERE type = ?`, model.LogTypeConsume).Scan(&other).Error)
	assert.True(t, gjson.Get(other, "admin_info.local_count_tokens").Bool())
}

func TestRelayGeminiTTSRefundsPreConsumeOnUnusableUpstreamResponse(t *testing.T) {
	audioData := base64.StdEncoding.EncodeToString(geminiTTSPCM)

	cases := []struct {
		name string
		body string
	}{
		{name: "malformed base64", body: `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"!!!"}}]}}]}`},
		{name: "untariffable usage", body: `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"` + audioData + `"}}]}}],"usageMetadata":{"promptTokenCount":100,"totalTokenCount":100}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGeminiTTSControllerFixture(t, 0)

			run := runGeminiTTSControllerRelay(t, db, dto.AudioRequest{
				Model: geminiTTSFlashModel, Input: "hello world", Voice: "alloy", ResponseFormat: "wav",
			}, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})

			assert.Equal(t, 1, run.upstreamCalls)
			assert.NotEqual(t, http.StatusOK, run.recorder.Code)

			require.NoError(t, run.inFlightErr)
			preConsumed := geminiTTSUserQuota - run.inFlightUserQuota
			require.Greater(t, preConsumed, 0)
			assert.Equal(t, preConsumed, run.inFlightTokenUsed)
			assert.Equal(t, geminiTTSUserQuota-preConsumed, run.inFlightTokenRemain)

			geminiTTSWaitForFullRefund(t, db)

			assert.Equal(t, 0, geminiTTSScalar(t, db, `SELECT used_quota FROM users WHERE id = ?`, geminiTTSUserId))
			assert.Equal(t, 0, geminiTTSScalar(t, db, `SELECT used_quota FROM channels WHERE id = ?`, geminiTTSChannelId))
			assert.Equal(t, 0, geminiTTSConsumeLogCount(t, db))
		})
	}
}

func TestRelayGeminiTTSRejectsInvalidRequestWithHTTP400AndNoRetry(t *testing.T) {
	speedTwo := 2.0

	cases := []struct {
		name    string
		request dto.AudioRequest
	}{
		{name: "empty input", request: dto.AudioRequest{Model: geminiTTSFlashModel, Voice: "alloy", ResponseFormat: "wav"}},
		{name: "unsupported speed", request: dto.AudioRequest{Model: geminiTTSFlashModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", Speed: &speedTwo}},
		{name: "sse stream format", request: dto.AudioRequest{Model: geminiTTSFlashModel, Input: "hi", Voice: "alloy", ResponseFormat: "wav", StreamFormat: "sse"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupGeminiTTSControllerFixture(t, 3)

			run := runGeminiTTSControllerRelay(t, db, tc.request, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			assert.Equal(t, http.StatusBadRequest, run.recorder.Code)
			assert.Equal(t, 0, run.upstreamCalls)
			assert.Equal(t, 1, run.channelTries)

			geminiTTSWaitForFullRefund(t, db)

			assert.Equal(t, 0, geminiTTSScalar(t, db, `SELECT used_quota FROM users WHERE id = ?`, geminiTTSUserId))
			assert.Equal(t, 0, geminiTTSConsumeLogCount(t, db))
		})
	}
}

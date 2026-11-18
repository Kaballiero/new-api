package taskruntime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	taskdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/model"
	service "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// telegramChat records the summaries the operators receive; a failing status
// rejects them.
type telegramChat struct {
	summaries []string
	status    int
}

func newTelegramChat(t *testing.T) *telegramChat {
	t.Helper()
	chat := &telegramChat{status: http.StatusOK}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Text string `json:"text"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		if chat.status == http.StatusOK {
			chat.summaries = append(chat.summaries, payload.Text)
		}
		w.WriteHeader(chat.status)
	}))
	t.Cleanup(server.Close)
	t.Setenv("TELEGRAM_GATEWAY_URL", server.URL)
	t.Setenv("TELEGRAM_GATEWAY_API_TOKEN", "gateway-token")
	return chat
}

// newOpenRouterSyncHold parks a delivered OpenRouter result without an upstream
// cost in manual reconciliation behind a real 50-quota wallet hold.
func newOpenRouterSyncHold(t *testing.T, info *relaycommon.RelayInfo) {
	t.Helper()
	taskfixture.Truncate(t)
	disableTaskFixtureArtifacts(t)
	taskfixture.SeedUser(t, info.UserId, 10000)
	taskfixture.SeedToken(t, info.UserId, info.UserId, info.TokenKey, 10000)
	baseURL := info.ChannelBaseUrl
	require.NoError(t, model.DB.Create(&model.Channel{Id: info.ChannelId, Name: "estimate-fixture", Type: constant.ChannelTypeOpenRouter, Key: info.ApiKey, BaseURL: &baseURL}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info.UserSetting.BillingPreference = "wallet_only"
	info.ForcePreConsume = true
	info.PriceData.Quota = 50
	require.NoError(t, BeginOpenRouterSyncTask(c, info, 50))
	require.Nil(t, service.PreConsumeBilling(c, 50, info))
	require.NoError(t, CommitOpenRouterSyncReservation(c, info))
	if info.NativeImage == nil {
		require.NoError(t, ConfigureOpenRouterSyncSubmission(info, baseURL+"/v1/generation", http.Header{"Authorization": []string{"Bearer " + info.ApiKey}}))
	} else {
		require.NoError(t, ConfigureOpenRouterSyncSubmission(info, "", nil))
	}
	require.NoError(t, CompleteOpenRouterSyncTask(c, info, []byte("result"), "application/json"))
	task, err := openRouterSyncTask(info)
	require.NoError(t, err)
	if info.NativeImage == nil {
		// The cost lookup ran out before OpenRouter published the cost.
		task.PrivateData.OpenRouterSync.Deadline = time.Now().Add(-time.Second).Unix()
		require.NoError(t, fixtureStore.UpdateOpenRouterSync(task, task.PrivateData.OpenRouterSync.Revision))
		require.NoError(t, pollOpenRouterSync(t.Context(), task))
	}
	task, err = openRouterSyncTask(info)
	require.NoError(t, err)
	require.Equal(t, "manual_reconciliation", OpenRouterSyncReconciliation(task)["billing_state"])
	require.True(t, task.PrivateData.OpenRouterSync.ResultReady)
}

func TestOpenRouterSyncEstimateSettlement(t *testing.T) {
	for _, tc := range []struct {
		name string
		// usageEstimateUSD is the text usage priced at the reserve tariffs.
		usageEstimateUSD     float64
		nativeImage          bool
		imagePricingUnknown  bool
		target, logType      int
		logQuota             int
		settled              bool
		wantSummarySubstring string
	}{
		{name: "text usage estimate below the reserve refunds the rest", usageEstimateUSD: .03, target: 30, logType: usagerecord.LogTypeRefund, logQuota: 20, settled: true, wantSummarySubstring: "costs charged by estimate"},
		{name: "text usage estimate above the reserve charges only the reserve", usageEstimateUSD: .08, target: 50, logType: usagerecord.LogTypeConsume, settled: true, wantSummarySubstring: "costs charged by estimate"},
		{name: "text without a tariff stays with root", target: 50, wantSummarySubstring: "manual reconciliation"},
		{name: "native image is charged its reserve", nativeImage: true, target: 50, logType: usagerecord.LogTypeConsume, settled: true, wantSummarySubstring: "costs charged by estimate"},
		{name: "native image without a published price stays with root", nativeImage: true, imagePricingUnknown: true, target: 50, wantSummarySubstring: "manual reconciliation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := newTelegramChat(t)
			const id = 991
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id, ChannelType: constant.ChannelTypeOpenRouter, ApiKey: "estimate-account", ChannelBaseUrl: "https://openrouter.invalid/api", UpstreamModelName: "vendor/model"},
				RelayMode:   relayconstant.RelayModeChatCompletions, UserId: id, TokenId: id, TokenKey: "estimate-token", OriginModelName: "model-alias", RequestId: "estimate-request",
				OpenRouterCost: &relaycommon.OpenRouterCostState{ChatText: true, Sent: true, GenerationID: "gen-estimate", UsageEstimateUSD: tc.usageEstimateUSD,
					Basis: common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: 1, QuotaPerUnit: 1000, EstimatedUSD: .05}},
			}
			if tc.nativeImage {
				info.RelayMode = relayconstant.RelayModeImagesGenerations
				info.OpenRouterCost = &relaycommon.OpenRouterCostState{Basis: info.OpenRouterCost.Basis}
				info.NativeImage = &relaycommon.NativeImageInfo{ChannelID: id, Model: "vendor/model", ProviderTag: "fixture", ReserveUSD: .05, EstimatedCostUSD: .05, PricingUnknown: tc.imagePricingUnknown}
			}
			newOpenRouterSyncHold(t, info)

			polling := NewPolling()
			polling.RunTasks(t.Context(), nil)
			polling.RunTasks(t.Context(), nil)

			task, err := openRouterSyncTask(info)
			require.NoError(t, err)
			require.Len(t, chat.summaries, 1, "one summary reports the pass, never again")
			assert.Contains(t, chat.summaries[0], tc.wantSummarySubstring)
			assert.Contains(t, chat.summaries[0], task.TaskID)
			assert.Equal(t, tc.target, task.Quota)
			assert.Equal(t, 10000-tc.target, getUserQuota(t, id))
			assert.Equal(t, 10000-tc.target, getTokenRemainQuota(t, id))
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Order("id").Find(&logs).Error)
			reconciliation := OpenRouterSyncReconciliation(task)
			if !tc.settled {
				assert.Equal(t, "manual_reconciliation", reconciliation["billing_state"])
				assert.True(t, task.OpenRouterSyncOperatorEligible(record.OpenRouterSyncOperatorSettle), "root can still settle the hold")
				assert.Len(t, logs, 1, "only the reservation")
				return
			}
			assert.Equal(t, "settled", reconciliation["billing_state"])
			assert.Equal(t, "estimate_settled", reconciliation["reason_code"])
			assert.Equal(t, taskdomain.SyncEstimateSettledFailReason, task.FailReason, "the apply leaves the pending retry")
			assert.Equal(t, false, reconciliation["cost_confirmed"], "an estimate is never a confirmed cost")
			assert.Equal(t, true, OpenRouterSyncAdminView(task)["cost_estimated"])
			assert.True(t, task.PrivateData.Reconciliation.UpstreamCostEstimated)
			assert.NotZero(t, task.PrivateData.Reconciliation.EstimateAlertedAt)
			decision := task.PrivateData.OpenRouterSync.OperatorDecision
			require.NotNil(t, decision)
			assert.Equal(t, record.OpenRouterSyncOperatorSettle, decision.Action)
			assert.Zero(t, decision.OperatorID)
			require.Len(t, logs, 2, "one reservation and one estimate settlement row, even without a delta")
			assert.Equal(t, tc.logType, logs[1].Type)
			assert.Equal(t, tc.logQuota, logs[1].Quota)
			assert.Equal(t, "OpenRouter estimated cost settlement", logs[1].Content)
			assert.Contains(t, logs[1].Other, `"upstream_cost_estimated":true`)
		})
	}
}

// finishOpenRouterVideo polls a v2 OpenRouter video whose reserve came from
// reservationSource to a success without its cost.
func finishOpenRouterVideo(t *testing.T, reservationSource string) *model.Task {
	t.Helper()
	taskfixture.Truncate(t)
	const id = 992
	taskfixture.SeedUser(t, id, 9950)
	taskfixture.SeedChannel(t, id)
	taskfixture.SeedToken(t, id, id, "video-estimate-token", 9950)
	taskfixture.SeedChargedAccounting(t, id, id, id, 50, 1)
	task := taskfixture.MakeTask(id, id, 50, id, BillingSourceWallet, 0)
	task.Platform = constant.TaskPlatformOpenRouterVideo
	task.PrivateData.UpstreamTaskID = "upstream-video"
	bc := task.PrivateData.BillingContext
	bc.SettlementMode = record.TaskSettlementOpenRouterCostV2
	bc.OpenRouterCost = &common.OpenRouterCostBasis{Version: record.TaskSettlementOpenRouterCostV2, EffectiveGroupRatio: 1, QuotaPerUnit: 1000, EstimatedUSD: .05}
	bc.OpenRouterOrderFacts = map[string]any{"reservation_source": reservationSource}
	task.PrivateData.Reconciliation = &record.TaskReconciliation{ReservedQuota: 50, Required: true}
	require.NoError(t, model.DB.Create(task).Error)

	adaptor := &scriptedPollingAdaptor{body: []byte(`{}`), parse: &relaycommon.TaskInfo{Status: string(record.TaskStatusSuccess), Url: "https://video.invalid/result.mp4"}}
	channel := &model.Channel{Id: id, Type: constant.ChannelTypeOpenRouter, Key: "fixture"}
	require.NoError(t, updateVideoSingleTask(t.Context(), adaptor, channel, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task}))
	return task
}

func TestOpenRouterVideoWithoutTariffReserveStaysWithRoot(t *testing.T) {
	for _, source := range []string{"manual", "configured_price"} {
		t.Run(source, func(t *testing.T) {
			chat := newTelegramChat(t)
			task := finishOpenRouterVideo(t, source)
			NewPolling().RunTasks(t.Context(), nil)
			var loaded model.Task
			require.NoError(t, model.DB.First(&loaded, task.ID).Error)
			assert.True(t, loaded.PrivateData.Reconciliation.Required)
			assert.False(t, loaded.PrivateData.Reconciliation.UpstreamCostEstimated)
			assert.False(t, loaded.PrivateData.Reconciliation.Finalized)
			assert.Equal(t, 50, loaded.Quota)
			assert.Empty(t, chat.summaries)
		})
	}
}

func TestOpenRouterVideoEstimateSettlement(t *testing.T) {
	chat := newTelegramChat(t)
	task := finishOpenRouterVideo(t, "live")
	const id = 992
	var loaded model.Task
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	assert.Equal(t, record.TaskStatus(record.TaskStatusSuccess), loaded.Status)
	assert.Equal(t, 50, loaded.Quota, "the delivered video is charged its reserve")
	assert.True(t, loaded.PrivateData.Reconciliation.Finalized)
	assert.True(t, loaded.PrivateData.Reconciliation.UpstreamCostEstimated)
	assert.Equal(t, 9950, getUserQuota(t, id))
	var settlement model.Log
	require.NoError(t, model.LOG_DB.Order("id desc").First(&settlement).Error)
	assert.Equal(t, "OpenRouter estimated cost settlement", settlement.Content)
	assert.Zero(t, settlement.Quota)

	// A summary that is not delivered never undoes the charge and is retried.
	chat.status = http.StatusBadGateway
	polling := NewPolling()
	polling.RunTasks(t.Context(), nil)
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	assert.Zero(t, loaded.PrivateData.Reconciliation.EstimateAlertedAt)
	assert.Equal(t, 50, loaded.Quota)

	chat.status = http.StatusOK
	polling.RunTasks(t.Context(), nil)
	polling.RunTasks(t.Context(), nil)
	require.Len(t, chat.summaries, 1)
	assert.Contains(t, chat.summaries[0], task.TaskID+" ("+task.Action+"): $")
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	assert.NotZero(t, loaded.PrivateData.Reconciliation.EstimateAlertedAt)
}

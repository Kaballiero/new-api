package controller

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaykittypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestWssCharacterization proves the approved WSS reconciliation contract.
// Client receipt is the deterministic barrier: the handler accounts for a
// response.done before it forwards that event to the client.
func TestWssCharacterization(t *testing.T) {
	oldGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldSQLitePath, oldMaster := common.SQLitePath, common.IsMasterNode
	oldCustomRate := operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = 100
	var testDB, testLogDB *gorm.DB
	t.Cleanup(func() {
		if testLogDB != nil && testLogDB != testDB {
			sqlDB, err := testLogDB.DB()
			if assert.NoError(t, err) {
				assert.NoError(t, sqlDB.Close())
			}
		}
		if testDB != nil {
			sqlDB, err := testDB.DB()
			if assert.NoError(t, err) {
				assert.NoError(t, sqlDB.Close())
			}
		}
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
		common.SetDatabaseTypes(oldMain, oldLog)
		common.SQLitePath, common.IsMasterNode = oldSQLitePath, oldMaster
		operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate = oldCustomRate
		gin.SetMode(oldGinMode)
	})
	mainDSN, logDSN := os.Getenv("FX_WSS_MAIN_DSN"), os.Getenv("FX_WSS_LOG_DSN")
	common.SQLitePath = t.TempDir() + "/wss-characterization.db"
	common.IsMasterNode = true
	if mainDSN == "" {
		t.Setenv("SQL_DSN", "local")
		t.Setenv("LOG_SQL_DSN", "")
	} else {
		t.Setenv("SQL_DSN", mainDSN)
		t.Setenv("LOG_SQL_DSN", logDSN)
	}
	require.NoError(t, model.InitDB())
	testDB = model.DB
	if mainDSN == "" {
		separateLogDB, err := gorm.Open(sqlite.Open(t.TempDir()+"/wss-characterization-log.db"), &gorm.Config{})
		require.NoError(t, err)
		model.LOG_DB = separateLogDB
		common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	} else {
		require.NoError(t, model.InitLogDB())
	}
	testLogDB = model.LOG_DB
	db, logDB := model.DB, model.LOG_DB
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	require.NoError(t, db.AutoMigrate(&model.ModelCostFXRow{}))
	require.NoError(t, db.AutoMigrate(&model.SubscriptionPlan{}, &model.UserSubscription{}, &model.SubscriptionPreConsumeRecord{}))
	publishWssFX(t, db, 100)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false

	oldGroups, oldModels := ratio_setting.GroupRatio2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModels))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"wss-char-model":1}`))

	t.Run("A_provider_usage_refusal_reconciles_unique_terminal_usage", func(t *testing.T) {
		r := newWssRun(t, db, logDB, 10_000_000_000, 3, false, []int{1, 2}, false)
		assert.Equal(t, ledger{wallet: 9_999_999_999, token: 2, tokenUsed: 1}, r.reserve)
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 1, tokenUsed: 2}, r.first)
		// A refusal produces no forwarded second event. Its exact pre-tail state
		// is private to the handler; terminal is sampled after WssHelper returns.
		r.releaseSecond(t)
		r.closeAndAwait(t)
		require.Error(t, r.err)
		assert.Equal(t, ledger{wallet: 9_999_999_997, token: 0, tokenUsed: 3}, r.terminal)
		assert.Equal(t, r.terminal, r.deferRefund)
		assert.Equal(t, 3, r.log.PromptTokens)
		assert.Equal(t, 3, r.log.Quota)
	})

	t.Run("B_live_GR_change_after_first_completed_debit", func(t *testing.T) {
		r := newWssRun(t, db, logDB, 10_000_000_000, 10_000_000_000, true, []int{1, 1}, false)
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, r.first)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1200000000}`))
		r.releaseSecond(t)
		r.awaitSecond(t)
		assert.Equal(t, ledger{wallet: 8_799_999_998, token: 8_799_999_998, tokenUsed: 1_200_000_002}, r.second)
		r.closeAndAwait(t)
		// Direct chunks use live GR, while terminal settlement keeps the request's
		// initial PriceData GR=1 and therefore records/settles aggregate 2.
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, r.terminal)
		assert.Equal(t, 2, r.log.PromptTokens)
		assert.Equal(t, 2, r.log.Quota)
		assert.Equal(t, r.terminal, r.deferRefund)
	})

	t.Run("B_live_GR_2_4B_saturates_real_helper_second_debit", func(t *testing.T) {
		r := newWssRun(t, db, logDB, 10_000_000_000, 10_000_000_000, true, []int{1, 1}, false)
		assert.Equal(t, ledger{wallet: 9_999_999_999, token: 9_999_999_999, tokenUsed: 1}, r.reserve)
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, r.first)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":2400000000}`))
		r.releaseSecond(t)
		r.closeAndAwait(t)
		require.Error(t, r.err)
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, r.terminal)
		assert.Equal(t, r.terminal, r.deferRefund)
		require.Len(t, r.logs, 1)
		assert.Equal(t, 2, r.log.PromptTokens)
		assert.Equal(t, 2, r.log.Quota)
		other, err := common.StrToMap(r.log.Other)
		require.NoError(t, err)
		assert.Equal(t, float64(2), other["text_input"])
		adminInfo, ok := other["admin_info"].(map[string]any)
		require.True(t, ok)
		assert.Contains(t, adminInfo, "quota_saturation")
	})

	t.Run("B_unchanged_source_clamp_direct_preconsume", func(t *testing.T) {
		// Supplementary direct service-boundary evidence; the real-helper probe
		// above is the required session characterization.
		r := newDirectWssFixture(t, db, logDB, 10_000_000_000, 10_000_000_000)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":2400000000}`))
		err := service.PreWssConsumeQuota(r.ctx, r.info, providerUsage(1))
		require.Error(t, err)
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 10_000_000_000, tokenUsed: 0}, readLedger(t, db))
		assert.NotNil(t, r.info.QuotaClamp)
	})

	t.Run("controlled_local_count_response_done_source", func(t *testing.T) {
		r := newWssRun(t, db, logDB, 10_000_000_000, 10_000_000_000, true, []int{0}, true)
		// The handler's nil-usage response.done branch counts the installed tool
		// through CountTokenRealtime, then forwards the done event after debit.
		assert.Equal(t, ledger{wallet: 9_999_999_999, token: 9_999_999_999, tokenUsed: 1}, r.reserve)
		assert.Equal(t, ledger{wallet: 9_999_999_981, token: 9_999_999_981, tokenUsed: 19}, r.first)
		r.closeAndAwait(t)
		assert.Equal(t, ledger{wallet: 9_999_999_982, token: 9_999_999_982, tokenUsed: 18}, r.terminal)
		assert.Equal(t, r.terminal, r.deferRefund)
		assert.Equal(t, r.log.PromptTokens, r.log.Quota)
		assert.Equal(t, 18, r.log.PromptTokens)
	})

	t.Run("controller_Relay_terminal_error_reconciles_without_retry_or_refund", func(t *testing.T) {
		resetWssTables(t, db, logDB, 10_000_000_000, 3, false)
		priority := int64(0)
		require.NoError(t, db.Create(&model.Ability{Group: "wss-char", Model: "wss-char-model", ChannelId: 1, Enabled: true, Priority: &priority}).Error)
		oldPreConsumed, oldRetries, oldErrorLogEnabled := common.PreConsumedQuota, common.RetryTimes, constant.ErrorLogEnabled
		common.PreConsumedQuota, common.RetryTimes, constant.ErrorLogEnabled = 1, 2, true
		t.Cleanup(func() {
			common.PreConsumedQuota, common.RetryTimes, constant.ErrorLogEnabled = oldPreConsumed, oldRetries, oldErrorLogEnabled
		})

		var output bytes.Buffer
		common.LogWriterMu.Lock()
		previousWriter, previousErrorWriter := gin.DefaultWriter, gin.DefaultErrorWriter
		gin.DefaultWriter, gin.DefaultErrorWriter = &output, &output
		common.LogWriterMu.Unlock()
		t.Cleanup(func() {
			common.LogWriterMu.Lock()
			gin.DefaultWriter, gin.DefaultErrorWriter = previousWriter, previousErrorWriter
			common.LogWriterMu.Unlock()
		})

		releaseSecond := make(chan struct{})
		stop := make(chan struct{})
		var releaseOnce, stopOnce sync.Once
		upstreamConnections := make(chan struct{}, 2)
		upstreamDone := make(chan struct{})
		var upstreamStarted, handlerStarted atomic.Bool
		upgrader := websocket.Upgrader{}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			upstreamStarted.Store(true)
			defer close(upstreamDone)
			conn, err := upgrader.Upgrade(w, req, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			upstreamConnections <- struct{}{}
			first, _ := common.Marshal(dto.RealtimeEvent{Type: dto.RealtimeEventTypeResponseDone, Response: &dto.RealtimeResponse{Usage: providerUsage(1)}})
			if err := conn.WriteMessage(websocket.TextMessage, first); err != nil {
				return
			}
			select {
			case <-releaseSecond:
			case <-stop:
				return
			}
			second, _ := common.Marshal(dto.RealtimeEvent{Type: dto.RealtimeEventTypeResponseDone, Response: &dto.RealtimeResponse{Usage: providerUsage(2)}})
			_ = conn.WriteMessage(websocket.TextMessage, second)
			_, _, _ = conn.ReadMessage()
		}))

		handlerDone := make(chan struct{})
		usedChannels := make(chan []string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			handlerStarted.Store(true)
			c, _ := gin.CreateTestContext(w)
			defer func() {
				usedChannels <- c.GetStringSlice("use_channel")
				close(handlerDone)
			}()
			if req.Body == nil {
				req.Body = http.NoBody
			}
			c.Request = req
			c.Set("channel_id", 1)
			c.Set("channel_type", constant.ChannelTypeOpenAI)
			c.Set("channel_name", "wss-char-channel")
			c.Set("auto_ban", false)
			common.SetContextKey(c, common.RequestIdKey, "wss-controller-terminal")
			common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
			common.SetContextKey(c, constant.ContextKeyUserId, 1)
			common.SetContextKey(c, constant.ContextKeyUserGroup, "wss-char")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "wss-char")
			common.SetContextKey(c, constant.ContextKeyTokenGroup, "wss-char")
			common.SetContextKey(c, constant.ContextKeyUserQuota, 10_000_000_000)
			common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{BillingPreference: "wallet_only"})
			common.SetContextKey(c, constant.ContextKeyTokenId, 1)
			common.SetContextKey(c, constant.ContextKeyTokenKey, "wss-char-token")
			common.SetContextKey(c, constant.ContextKeyTokenUnlimited, false)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "wss-char-model")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelId, 1)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test")
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			Relay(c, relaykittypes.RelayFormatOpenAIRealtime)
		}))
		var client *websocket.Conn
		t.Cleanup(func() {
			releaseOnce.Do(func() { close(releaseSecond) })
			stopOnce.Do(func() { close(stop) })
			if client != nil {
				_ = client.Close()
			}
			server.CloseClientConnections()
			upstream.CloseClientConnections()
			server.Close()
			upstream.Close()
			if handlerStarted.Load() {
				select {
				case <-handlerDone:
				case <-time.After(5 * time.Second):
					t.Errorf("Relay handler did not stop during cleanup")
				}
			}
			if upstreamStarted.Load() {
				select {
				case <-upstreamDone:
				case <-time.After(5 * time.Second):
					t.Errorf("upstream websocket peer did not stop during cleanup")
				}
			}
		})
		client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/realtime", nil)
		require.NoError(t, err)
		require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, _, err = client.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 1, tokenUsed: 2}, readLedger(t, db))
		releaseOnce.Do(func() { close(releaseSecond) })
		select {
		case <-handlerDone:
		case <-time.After(5 * time.Second):
			t.Fatal("Relay did not return")
		}
		assert.Equal(t, []string{"1"}, <-usedChannels)
		assert.Equal(t, ledger{wallet: 9_999_999_997, token: 0, tokenUsed: 3}, readLedger(t, db))
		select {
		case <-upstreamConnections:
		default:
			t.Fatal("upstream was not called")
		}
		select {
		case <-upstreamConnections:
			t.Fatal("unexpected retry upstream connection")
		default:
		}
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, 3, logs[0].PromptTokens)
		assert.Equal(t, 3, logs[0].Quota)
		var errorLogs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeError).Find(&errorLogs).Error)
		require.Len(t, errorLogs, 1)
		assert.Contains(t, errorLogs[0].Content, "token quota is not enough")
		logOutput := output.String()
		assert.Contains(t, logOutput, "relay error:")
		assert.NotContains(t, logOutput, "请求失败, 返还预扣费")
	})

	t.Run("subscription_session_uses_real_reservation_source_and_reconciles", func(t *testing.T) {
		resetWssTables(t, db, logDB, 10_000_000_000, 100, false)
		subscription := seedWssSubscription(t, db, 1, 100)
		c, info := newSubscriptionWssInfo(t, "wss-subscription", subscription.Id)
		require.Nil(t, service.PreConsumeBilling(c, 1, info))
		assert.EqualValues(t, 1, subscriptionAmountUsed(t, db, subscription.Id))
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 99, tokenUsed: 1}, readLedger(t, db))

		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.EqualValues(t, 2, subscriptionAmountUsed(t, db, subscription.Id))
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 98, tokenUsed: 2}, readLedger(t, db))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.EqualValues(t, 3, subscriptionAmountUsed(t, db, subscription.Id))
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 97, tokenUsed: 3}, readLedger(t, db))

		require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), ""))
		assert.EqualValues(t, 2, subscriptionAmountUsed(t, db, subscription.Id))
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 98, tokenUsed: 2}, readLedger(t, db))
		var record model.SubscriptionPreConsumeRecord
		require.NoError(t, db.Where("request_id = ?", info.RequestId).First(&record).Error)
		assert.Equal(t, subscription.Id, record.UserSubscriptionId)
		assert.EqualValues(t, 1, record.PreConsumed)
		assert.Equal(t, "consumed", record.Status)
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, 2, logs[0].Quota)

		require.NoError(t, service.SettleWssBilling(c, info, 2))
		info.Billing.Refund(c)
		assert.False(t, info.Billing.NeedsRefund())
		assert.EqualValues(t, 2, subscriptionAmountUsed(t, db, subscription.Id))
		assert.Equal(t, ledger{wallet: 10_000_000_000, token: 98, tokenUsed: 2}, readLedger(t, db))
	})

	t.Run("partial_chunk_keeps_successful_funding_leg", func(t *testing.T) {
		injected := errors.New("injected token update failure")
		var armed atomic.Bool
		var hits atomic.Int32
		callbackName := "wss_partial_chunk_token_failure"
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" && armed.CompareAndSwap(true, false) {
				hits.Add(1)
				tx.AddError(injected)
			}
		}))
		t.Cleanup(func() { db.Callback().Update().Remove(callbackName) })

		for _, tc := range []struct {
			name          string
			subscription  bool
			wantDirect    ledger
			wantFinal     ledger
			wantSubDirect int64
			wantSubFinal  int64
		}{
			{name: "wallet", wantDirect: ledger{wallet: 96, token: 98, tokenUsed: 2}, wantFinal: ledger{wallet: 97, token: 97, tokenUsed: 3}},
			{name: "subscription", subscription: true, wantDirect: ledger{wallet: 100, token: 98, tokenUsed: 2}, wantFinal: ledger{wallet: 100, token: 97, tokenUsed: 3}, wantSubDirect: 4, wantSubFinal: 3},
		} {
			t.Run(tc.name, func(t *testing.T) {
				hits.Store(0)
				resetWssTables(t, db, logDB, 100, 100, false)
				c, info := newDirectWssFixture(t, db, logDB, 100, 100).ctx, testRelayInfo(false)
				var subscription *model.UserSubscription
				if tc.subscription {
					subscription = seedWssSubscription(t, db, 1, 100)
					c, info = newSubscriptionWssInfo(t, "wss-partial-chunk-subscription", subscription.Id)
				}
				require.Nil(t, service.PreConsumeBilling(c, 1, info))
				require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
				wantFirst := ledger{wallet: 98, token: 98, tokenUsed: 2}
				if tc.subscription {
					wantFirst.wallet = 100
				}
				assert.Equal(t, wantFirst, readLedger(t, db))
				if tc.subscription {
					assert.EqualValues(t, 2, subscriptionAmountUsed(t, db, subscription.Id))
				}

				armed.Store(true)
				err := service.PreWssConsumeQuota(c, info, providerUsage(2))
				require.ErrorIs(t, err, injected)
				assert.Equal(t, int32(1), hits.Load())
				assert.Equal(t, tc.wantDirect, readLedger(t, db))
				if tc.subscription {
					assert.Equal(t, tc.wantSubDirect, subscriptionAmountUsed(t, db, subscription.Id))
				}

				require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(3), ""))
				assert.Equal(t, tc.wantFinal, readLedger(t, db))
				if tc.subscription {
					assert.Equal(t, tc.wantSubFinal, subscriptionAmountUsed(t, db, subscription.Id))
				}
				var logs []model.Log
				require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				assert.Equal(t, 3, logs[0].Quota)
				require.NoError(t, service.SettleWssBilling(c, info, 3))
				info.Billing.Refund(c)
				assert.Equal(t, tc.wantFinal, readLedger(t, db))
			})
		}
	})

	t.Run("partial_terminal_token_failure_does_not_repeat_funding", func(t *testing.T) {
		resetWssTables(t, db, logDB, 10_000_000_000, 10_000_000_000, false)
		c, info := newDirectWssFixture(t, db, logDB, 10_000_000_000, 10_000_000_000).ctx, testRelayInfo(false)
		require.Nil(t, service.PreConsumeBilling(c, 1, info))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1200000000}`))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))

		injected := errors.New("injected terminal token update failure")
		var armed atomic.Bool
		var hits atomic.Int32
		callbackName := "wss_partial_terminal_token_failure"
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" && armed.CompareAndSwap(true, false) {
				hits.Add(1)
				tx.AddError(injected)
			}
		}))
		t.Cleanup(func() { db.Callback().Update().Remove(callbackName) })
		armed.Store(true)

		err := service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), "")
		require.ErrorIs(t, err, injected)
		assert.Equal(t, int32(1), hits.Load())
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 8_799_999_998, tokenUsed: 1_200_000_002}, readLedger(t, db))
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		assert.Empty(t, logs)

		require.NoError(t, service.SettleWssBilling(c, info, 2))
		info.Billing.Refund(c)
		assert.False(t, info.Billing.NeedsRefund())
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, readLedger(t, db))
		require.NoError(t, service.SettleWssBilling(c, info, 2))
		assert.Equal(t, ledger{wallet: 9_999_999_998, token: 9_999_999_998, tokenUsed: 2}, readLedger(t, db))
	})

	t.Run("F2_clamped_second_chunk_retains_terminal_error_and_snapshot", func(t *testing.T) {
		publishWssFX(t, db, 200)
		t.Cleanup(func() { publishWssFX(t, db, 100) })
		resetWssTables(t, db, logDB, 10_000_000_000, 10_000_000_000, false)
		c, info := newDirectWssFixture(t, db, logDB, 10_000_000_000, 10_000_000_000).ctx, testRelayInfo(false)
		factor, err := service.CaptureBillingFX(info)
		require.NoError(t, err)
		assert.Equal(t, 2.0, factor)
		assert.Equal(t, 2.0, info.BillingFXFactor)
		require.Nil(t, service.PreConsumeBilling(c, 2, info))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, 200.0, info.BillingFXRate)
		assert.Equal(t, 2.0, info.BillingFXFactor)
		assert.Equal(t, ledger{wallet: 9_999_999_996, token: 9_999_999_996, tokenUsed: 4}, readLedger(t, db))

		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1200000000}`))
		err = service.PreWssConsumeQuota(c, info, providerUsage(1))
		require.Error(t, err)
		require.NotNil(t, info.QuotaClamp)
		assert.Equal(t, ledger{wallet: 9_999_999_996, token: 9_999_999_996, tokenUsed: 4}, readLedger(t, db))

		require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), ""))
		assert.Equal(t, ledger{wallet: 9_999_999_996, token: 9_999_999_996, tokenUsed: 4}, readLedger(t, db))
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, 4, logs[0].Quota)
	})

	t.Run("WSS_retains_captured_F2_after_RAM_changes", func(t *testing.T) {
		publishWssFX(t, db, 200)
		t.Cleanup(func() { publishWssFX(t, db, 100) })
		resetWssTables(t, db, logDB, 10_000_000_000, 10_000_000_000, false)
		c, info := newDirectWssFixture(t, db, logDB, 10_000_000_000, 10_000_000_000).ctx, testRelayInfo(false)
		info.PriceData.BillingGroupRatio = 2
		require.Nil(t, service.PreConsumeBilling(c, 2, info))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, ledger{wallet: 9_999_999_996, token: 9_999_999_996, tokenUsed: 4}, readLedger(t, db))

		publishWssFX(t, db, 90)
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, 200.0, info.BillingFXRate)
		assert.Equal(t, 2.0, info.BillingFXFactor)
		assert.Equal(t, ledger{wallet: 9_999_999_994, token: 9_999_999_994, tokenUsed: 6}, readLedger(t, db))

		require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), ""))
		assert.Equal(t, ledger{wallet: 9_999_999_996, token: 9_999_999_996, tokenUsed: 4}, readLedger(t, db))
	})

	t.Run("WSS_keeps_F2_separate_from_extreme_live_group_ratio", func(t *testing.T) {
		publishWssFX(t, db, 200)
		t.Cleanup(func() { publishWssFX(t, db, 100) })
		c, info := newDirectWssFixture(t, db, logDB, 100, 100).ctx, testRelayInfo(false)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1e308}`))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"wss-char-model":1e-308}`))
		t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"wss-char-model":1}`)) })
		assert.Equal(t, 1e308, ratio_setting.GetGroupRatio("wss-char"))
		modelRatio, _, _ := ratio_setting.GetModelRatio("wss-char-model")
		assert.Equal(t, 1e-308, modelRatio)
		factor, err := service.CaptureBillingFX(info)
		require.NoError(t, err)
		assert.Equal(t, 2.0, factor)
		require.Nil(t, service.PreConsumeBilling(c, 2, info))
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, 2.0, info.BillingFXFactor)
		assert.Nil(t, info.QuotaClamp)
		assert.Equal(t, ledger{wallet: 96, token: 96, tokenUsed: 4}, readLedger(t, db))
	})

	t.Run("sessionless_WSS_reconciles_actual_direct_charge_without_reservation", func(t *testing.T) {
		resetWssTables(t, db, logDB, 100, 100, false)
		c, info := newDirectWssFixture(t, db, logDB, 100, 100).ctx, testRelayInfo(false)
		require.Nil(t, info.Billing)
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, ledger{wallet: 99, token: 99, tokenUsed: 1}, readLedger(t, db))
		require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(1), ""))
		assert.Equal(t, ledger{wallet: 99, token: 99, tokenUsed: 1}, readLedger(t, db))
		require.NoError(t, service.SettleWssBilling(c, info, 1))
		assert.Equal(t, ledger{wallet: 99, token: 99, tokenUsed: 1}, readLedger(t, db))
	})

	t.Run("sessionless_terminal_token_failure_keeps_funding_on_reentry", func(t *testing.T) {
		resetWssTables(t, db, logDB, 100, 100, false)
		c, info := newDirectWssFixture(t, db, logDB, 100, 100).ctx, testRelayInfo(false)
		require.Nil(t, info.Billing)
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, ledger{wallet: 99, token: 99, tokenUsed: 1}, readLedger(t, db))

		injected := errors.New("injected sessionless terminal token failure")
		var armed atomic.Bool
		callbackName := "wss_sessionless_terminal_token_failure"
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" && armed.CompareAndSwap(true, false) {
				tx.AddError(injected)
			}
		}))
		t.Cleanup(func() { db.Callback().Update().Remove(callbackName) })
		armed.Store(true)

		err := service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), "")
		require.ErrorIs(t, err, injected)
		// Final Q=2, reservation R=0, successful direct D=1: funding settles
		// the remaining unit before the injected token leg fails.
		assert.Equal(t, ledger{wallet: 98, token: 99, tokenUsed: 1}, readLedger(t, db))
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		assert.Empty(t, logs)

		require.NoError(t, service.SettleWssBilling(c, info, 2))
		assert.Equal(t, ledger{wallet: 98, token: 98, tokenUsed: 2}, readLedger(t, db))
		require.NoError(t, service.SettleWssBilling(c, info, 2))
		assert.Equal(t, ledger{wallet: 98, token: 98, tokenUsed: 2}, readLedger(t, db))
	})

	t.Run("playground_WSS_reconciles_without_a_token_leg", func(t *testing.T) {
		resetWssTables(t, db, logDB, 100, 100, false)
		c, info := newDirectWssFixture(t, db, logDB, 100, 100).ctx, testRelayInfo(false)
		info.IsPlayground = true
		require.Nil(t, info.Billing)
		require.NoError(t, service.PreWssConsumeQuota(c, info, providerUsage(1)))
		assert.Equal(t, ledger{wallet: 99, token: 100, tokenUsed: 0}, readLedger(t, db))

		require.NoError(t, service.PostWssConsumeQuota(c, info, info.OriginModelName, providerUsage(2), ""))
		// Final Q=2, reservation R=0, direct D=1. Playground must adjust only
		// funding and must not synthesize a token debit.
		assert.Equal(t, ledger{wallet: 98, token: 100, tokenUsed: 0}, readLedger(t, db))
		var logs []model.Log
		require.NoError(t, logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, 2, logs[0].Quota)

		require.NoError(t, service.SettleWssBilling(c, info, 2))
		assert.Equal(t, ledger{wallet: 98, token: 100, tokenUsed: 0}, readLedger(t, db))
	})

	t.Run("normal_client_close_keeps_concurrent_charge_failure", func(t *testing.T) {
		r := newWssRun(t, db, logDB, 100, 100, false, []int{1, 2}, false)
		chargeErr := errors.New("concurrent token charge failure")
		settlementErr := errors.New("terminal token settlement failure")
		var armed atomic.Bool
		var chargeHits atomic.Int32
		chargeStarted := make(chan struct{})
		allowChargeFailure := make(chan struct{})
		callbackName := "wss_close_concurrent_charge_failure"
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Table != "tokens" || !armed.Load() {
				return
			}
			if chargeHits.Add(1) == 1 {
				close(chargeStarted)
				<-allowChargeFailure
				tx.AddError(chargeErr)
				return
			}
			tx.AddError(settlementErr)
		}))
		t.Cleanup(func() { db.Callback().Update().Remove(callbackName) })

		armed.Store(true)
		r.releaseSecond(t)
		select {
		case <-chargeStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("second charge did not reach the deterministic barrier")
		}
		r.closeClientNormally(t)
		close(allowChargeFailure)
		r.closeAndAwait(t)

		require.ErrorIs(t, r.err, chargeErr)
		require.ErrorIs(t, r.err, settlementErr)
		assert.Equal(t, int32(2), chargeHits.Load())
	})

	t.Run("transport_UsePrice_skips_direct_chunks_and_settles_saved_price_once", func(t *testing.T) {
		r := newWssRunConfigured(t, db, logDB, 100, 100, false, []int{1}, false, func(info *relaycommon.RelayInfo) {
			info.PriceData.UsePrice = true
			info.PriceData.ModelPrice = 0.000002
		})
		assert.Equal(t, ledger{wallet: 99, token: 99, tokenUsed: 1}, r.reserve)
		r.closeAndAwait(t)
		require.Nil(t, r.err)
		assert.Equal(t, r.reserve, r.terminal)
		require.Len(t, r.logs, 1)
		assert.Equal(t, 1, r.log.Quota)
	})

	t.Run("transport_tiered_final_clamp_has_no_success_log", func(t *testing.T) {
		r := newWssRunConfigured(t, db, logDB, 10_000_000_000, 10_000_000_000, false, []int{10_001}, false, func(info *relaycommon.RelayInfo) {
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{
				BillingMode:               "tiered_expr",
				ExprString:                `p > 10000 ? 1e20 : 0`,
				ExprHash:                  billingexpr.ExprHashString(`p > 10000 ? 1e20 : 0`),
				GroupRatio:                1,
				EstimatedQuotaBeforeGroup: 1,
				EstimatedQuotaAfterGroup:  1,
				QuotaPerUnit:              500_000,
			}
		})
		r.closeAndAwait(t)
		require.Error(t, r.err)
		assert.Empty(t, r.logs)
		assert.NotEqual(t, 2_147_483_647, r.terminal.tokenUsed)
	})
}

type ledger struct{ wallet, token, tokenUsed int }
type wssRun struct {
	t                                             *testing.T
	db                                            *gorm.DB
	logDB                                         *gorm.DB
	client                                        *websocket.Conn
	upstream, server                              *httptest.Server
	release                                       chan struct{}
	closePeer                                     chan struct{}
	stop                                          chan struct{}
	releaseOnce, closePeerOnce, stopOnce          sync.Once
	result                                        chan wssResult
	peerErr                                       chan error
	upstreamDone, handlerDone                     chan struct{}
	upstreamStarted, handlerStarted               atomic.Bool
	reserve, first, second, terminal, deferRefund ledger
	log                                           model.Log
	logs                                          []model.Log
	err                                           *relaykittypes.NewAPIError
}
type wssResult struct {
	terminal, deferRefund ledger
	log                   model.Log
	logs                  []model.Log
	err                   *relaykittypes.NewAPIError
}
type directWssFixture struct {
	ctx  *gin.Context
	info *relaycommon.RelayInfo
}

func newWssRun(t *testing.T, db, logDB *gorm.DB, wallet, token int, unlimited bool, usages []int, local bool) *wssRun {
	return newWssRunConfigured(t, db, logDB, wallet, token, unlimited, usages, local, nil)
}

func newWssRunConfigured(t *testing.T, db, logDB *gorm.DB, wallet, token int, unlimited bool, usages []int, local bool, configure func(*relaycommon.RelayInfo)) *wssRun {
	t.Helper()
	resetWssTables(t, db, logDB, wallet, token, unlimited)
	r := &wssRun{
		t: t, db: db, logDB: logDB,
		release: make(chan struct{}), closePeer: make(chan struct{}), stop: make(chan struct{}),
		result: make(chan wssResult, 1), peerErr: make(chan error, 4),
		upstreamDone: make(chan struct{}), handlerDone: make(chan struct{}),
	}
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.upstreamStarted.Store(true)
		defer close(r.upstreamDone)
		conn, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			r.recordPeerErr(err)
			return
		}
		defer func() {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "test complete"), time.Now().Add(time.Second))
			_ = conn.Close()
		}()
		for i, usage := range usages {
			var event dto.RealtimeEvent
			if local {
				event = dto.RealtimeEvent{Type: dto.RealtimeEventTypeResponseDone, Response: &dto.RealtimeResponse{}}
			} else {
				event = dto.RealtimeEvent{Type: dto.RealtimeEventTypeResponseDone, Response: &dto.RealtimeResponse{Usage: providerUsage(usage)}}
			}
			payload, err := common.Marshal(event)
			if err != nil {
				r.recordPeerErr(err)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				r.recordPeerErr(err)
				return
			}
			if i == 0 && len(usages) > 1 {
				select {
				case <-r.release:
				case <-r.stop:
					return
				}
			}
		}
		select {
		case <-r.closePeer:
		case <-r.stop:
		}
	}))
	r.upstream = upstream
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.handlerStarted.Store(true)
		defer close(r.handlerDone)
		client, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			r.recordPeerErr(err)
			return
		}
		defer func() {
			_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "test complete"), time.Now().Add(time.Second))
			_ = client.Close()
		}()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
		common.SetContextKey(c, constant.ContextKeyChannelKey, "test")
		common.SetContextKey(c, constant.ContextKeyChannelId, 1)
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "wss-char-model")
		info := testRelayInfo(unlimited)
		if configure != nil {
			configure(info)
		}
		if _, err := service.CaptureBillingFX(info); err != nil {
			r.recordPeerErr(err)
			return
		}
		info.ClientWs = client
		if local {
			info.IsFirstRequest = false
			info.RealtimeTools = []dto.RealTimeTool{{Type: "function", Name: "x", Description: "x", Parameters: map[string]any{"type": "object"}}}
		}
		if billingErr := service.PreConsumeBilling(c, 1, info); billingErr != nil {
			r.recordPeerErr(billingErr)
			return
		}
		reserve, err := readLedgerValue(db)
		if err != nil {
			r.recordPeerErr(err)
			return
		}
		r.reserve = reserve
		wssErr := relay.WssHelper(c, info)
		terminal, err := readLedgerValue(db)
		if err != nil {
			r.recordPeerErr(err)
			return
		}
		var logs []model.Log
		if err := logDB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error; err != nil {
			r.recordPeerErr(err)
			return
		}
		var log model.Log
		if len(logs) > 0 {
			log = logs[len(logs)-1]
		}
		info.Billing.Refund(c)
		deferRefund, err := readLedgerValue(db)
		if err != nil {
			r.recordPeerErr(err)
			return
		}
		r.result <- wssResult{terminal: terminal, deferRefund: deferRefund, log: log, logs: logs, err: wssErr}
	}))
	r.server = server
	t.Cleanup(r.cleanup)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	r.client = client
	r.awaitFirst(t)
	return r
}
func (r *wssRun) awaitFirst(t *testing.T) {
	t.Helper()
	r.readForwarded(t)
	r.first = readLedger(t, r.db)
}
func (r *wssRun) releaseSecond(t *testing.T) {
	t.Helper()
	r.releaseOnce.Do(func() { close(r.release) })
}
func (r *wssRun) awaitSecond(t *testing.T) {
	t.Helper()
	r.readForwarded(t)
	r.second = readLedger(t, r.db)
}
func (r *wssRun) closeAndAwait(t *testing.T) {
	t.Helper()
	r.releaseSecond(t)
	r.closePeerOnce.Do(func() { close(r.closePeer) })
	select {
	case err := <-r.peerErr:
		require.NoError(t, err)
	case got := <-r.result:
		r.terminal, r.deferRefund, r.log, r.logs, r.err = got.terminal, got.deferRefund, got.log, got.logs, got.err
		if got.err != nil {
			t.Logf("WssHelper returned: %v", got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for WssHelper")
	}
}
func (r *wssRun) closeClientNormally(t *testing.T) {
	t.Helper()
	require.NoError(t, r.client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "test complete"), time.Now().Add(5*time.Second)))
}
func (r *wssRun) cleanup() {
	r.releaseOnce.Do(func() { close(r.release) })
	r.closePeerOnce.Do(func() { close(r.closePeer) })
	r.stopOnce.Do(func() { close(r.stop) })
	if r.client != nil {
		_ = r.client.Close()
	}
	if r.server != nil {
		r.server.CloseClientConnections()
	}
	if r.upstream != nil {
		r.upstream.CloseClientConnections()
	}
	if r.server != nil {
		r.server.Close()
	}
	if r.upstream != nil {
		r.upstream.Close()
	}
	if r.handlerStarted.Load() {
		select {
		case <-r.handlerDone:
		case <-time.After(5 * time.Second):
			r.t.Errorf("websocket handler did not stop during cleanup")
		}
	}
	if r.upstreamStarted.Load() {
		select {
		case <-r.upstreamDone:
		case <-time.After(5 * time.Second):
			r.t.Errorf("upstream websocket peer did not stop during cleanup")
		}
	}
}
func (r *wssRun) recordPeerErr(err error) {
	select {
	case r.peerErr <- err:
	default:
	}
}
func (r *wssRun) readForwarded(t *testing.T) {
	t.Helper()
	require.NoError(t, r.client.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := r.client.ReadMessage()
	require.NoError(t, err)
}
func newDirectWssFixture(t *testing.T, db, logDB *gorm.DB, wallet, token int) directWssFixture {
	t.Helper()
	resetWssTables(t, db, logDB, wallet, token, true)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	return directWssFixture{ctx: c, info: testRelayInfo(true)}
}
func resetWssTables(t *testing.T, db, logDB *gorm.DB, wallet, token int, unlimited bool) {
	t.Helper()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"wss-char":1}`))
	require.NoError(t, logDB.Exec("DELETE FROM logs").Error)
	require.NoError(t, db.Exec("DELETE FROM subscription_pre_consume_records").Error)
	require.NoError(t, db.Exec("DELETE FROM user_subscriptions").Error)
	require.NoError(t, db.Exec("DELETE FROM subscription_plans").Error)
	require.NoError(t, db.Exec("DELETE FROM abilities").Error)
	require.NoError(t, db.Exec("DELETE FROM tokens").Error)
	require.NoError(t, db.Exec("DELETE FROM users").Error)
	require.NoError(t, db.Exec("DELETE FROM channels").Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "wss-char-channel", Type: constant.ChannelTypeOpenAI, Key: "test"}).Error)
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "wss-char", Password: "placeholder", Quota: wallet, Group: "wss-char"}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "wss-char-token", Name: "wss-char-token", RemainQuota: token, UnlimitedQuota: unlimited}).Error)
}

func seedWssSubscription(t *testing.T, db *gorm.DB, userID int, amountTotal int64) *model.UserSubscription {
	t.Helper()
	plan := model.SubscriptionPlan{
		Title:               "wss-characterization",
		Enabled:             true,
		TotalAmount:         amountTotal,
		QuotaResetPeriod:    model.SubscriptionResetNever,
		AllowWalletOverflow: common.GetPointer(false),
	}
	require.NoError(t, db.Create(&plan).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	subscription := &model.UserSubscription{
		UserId:              userID,
		PlanId:              plan.Id,
		AmountTotal:         amountTotal,
		AmountUsed:          0,
		StartTime:           time.Now().Add(-time.Hour).Unix(),
		EndTime:             time.Now().Add(time.Hour).Unix(),
		Status:              "active",
		Source:              "admin",
		AllowWalletOverflow: false,
	}
	require.NoError(t, db.Create(subscription).Error)
	t.Cleanup(func() { model.InvalidateSubscriptionPlanCache(plan.Id) })
	return subscription
}

func publishWssFX(t *testing.T, db *gorm.DB, usd float64) {
	t.Helper()
	var row model.ModelCostFXRow
	err := db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effectiveAt, fetchedAt, currentVersion := row.EffectiveAt, row.FetchedAt, int64(0)
	if current, err := model.CurrentModelCostFX("cbr-xml-daily.ru"); err == nil {
		currentVersion = current.Version
		if current.EffectiveAt > effectiveAt {
			effectiveAt = current.EffectiveAt
		}
		if current.FetchedAt > fetchedAt {
			fetchedAt = current.FetchedAt
		}
	}
	for first := true; first || row.Version <= currentVersion; first = false {
		effectiveAt++
		fetchedAt++
		require.NoError(t, model.SaveModelCostFX(context.Background(), model.ModelCostFXSnapshot{
			Source:      "cbr-xml-daily.ru",
			EffectiveAt: effectiveAt,
			FetchedAt:   fetchedAt,
			Rates:       map[string]float64{"USD": usd},
		}, row.Version))
		require.NoError(t, db.Where("source = ?", "cbr-xml-daily.ru").First(&row).Error)
	}
	published, err := model.CurrentModelCostFX("cbr-xml-daily.ru")
	require.NoError(t, err)
	require.Equal(t, usd, published.Rates["USD"])
}

func newSubscriptionWssInfo(t *testing.T, requestID string, subscriptionID int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, common.RequestIdKey, requestID)
	c.Set("token_quota", 100)
	info := testRelayInfo(false)
	info.RequestId = requestID
	info.UserQuota = 10_000_000_000
	info.UserSetting.BillingPreference = "subscription_only"
	info.SubscriptionId = subscriptionID
	return c, info
}

func subscriptionAmountUsed(t *testing.T, db *gorm.DB, subscriptionID int) int64 {
	t.Helper()
	var subscription model.UserSubscription
	require.NoError(t, db.First(&subscription, subscriptionID).Error)
	return subscription.AmountUsed
}
func testRelayInfo(unlimited bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "wss-char-token", TokenUnlimited: unlimited, UsingGroup: "wss-char", UserGroup: "wss-char", OriginModelName: "wss-char-model", RelayMode: relayconstant.RelayModeRealtime, RequestURLPath: "/v1/realtime", ForcePreConsume: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1}, PriceData: types.PriceData{ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	info.UserSetting.BillingPreference = "wallet_only"
	return info
}
func providerUsage(n int) *dto.RealtimeUsage {
	return &dto.RealtimeUsage{TotalTokens: n, InputTokens: n, InputTokenDetails: dto.InputTokenDetails{TextTokens: n}}
}
func readLedger(t *testing.T, db *gorm.DB) ledger {
	t.Helper()
	value, err := readLedgerValue(db)
	require.NoError(t, err)
	return value
}
func readLedgerValue(db *gorm.DB) (ledger, error) {
	var user model.User
	var token model.Token
	if err := db.First(&user, 1).Error; err != nil {
		return ledger{}, err
	}
	if err := db.First(&token, 1).Error; err != nil {
		return ledger{}, err
	}
	return ledger{wallet: user.Quota, token: token.RemainQuota, tokenUsed: token.UsedQuota}, nil
}

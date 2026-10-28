package batchruntime_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	math "github.com/QuantumNous/new-api/internal/adapter/billing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/batch"
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	account "github.com/QuantumNous/new-api/internal/adapter/http/batchaccount"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	rediscache "github.com/QuantumNous/new-api/internal/adapter/redis/batch"
	prices "github.com/QuantumNous/new-api/internal/adapter/settings/batch"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	apikeystorage "github.com/QuantumNous/new-api/internal/app/apikeystorage"
	runtime "github.com/QuantumNous/new-api/internal/app/batchruntime"
	billingapp "github.com/QuantumNous/new-api/internal/app/billing"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	batchdto "github.com/QuantumNous/new-api/internal/batch/dto"
	"github.com/QuantumNous/new-api/internal/batch/recover_batch_identity"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/batch"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBatchWorkerLifecycle(t *testing.T) {
	require.NoError(t, i18n.Init())
	general := operation_setting.GetGeneralSetting()
	previousRate := general.CustomCurrencyExchangeRate
	general.CustomCurrencyExchangeRate = 100
	t.Cleanup(func() { general.CustomCurrencyExchangeRate = previousRate })
	for _, mode := range []string{"dispatch_revoked_cache_hold", "dispatch_revoked", "dispatch_expired", "dispatch_deleted", "dispatch_revoked_after_preflight", "dispatch_revoked_after_marker", "success", "provider_routing", "corrupt_contract", "ambiguous", "foreign_identity", "rejected", "malicious_rejected", "endpoint_rejected", "oversized_rejected", "provider_failed", "provider_failed_settlement_error", "aggregate_without_item_cost", "v2_aggregate", "v2_foreign_identity", "v2_null_counts", "v2_bad_counts", "v2_missing_cost", "v2_withheld"} {
		t.Run(mode, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldLogDB := model.LOG_DB
			model.LOG_DB = db
			t.Cleanup(func() { model.LOG_DB = oldLogDB })
			oldDB, oldRedis, oldMemory := model.DB, common.RedisEnabled, common.MemoryCacheEnabled
			model.DB, common.RedisEnabled, common.MemoryCacheEnabled = db, false, false
			t.Cleanup(func() {
				model.DB, common.RedisEnabled, common.MemoryCacheEnabled = oldDB, oldRedis, oldMemory
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.BatchLogEvent{}, &model.BatchLogReceipt{}, &model.Log{}))
			if mode == "dispatch_revoked_cache_hold" {
				r := miniredis.RunT(t)
				client := redis.NewClient(&redis.Options{Addr: r.Addr()})
				oldRDB := common.RDB
				common.RedisEnabled, common.RDB = true, client
				t.Cleanup(func() { _ = client.Close(); common.RDB = oldRDB })
			}
			var tokenID int
			var posts atomic.Int32
			var polls atomic.Int32
			var nativeCustomID string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					w.WriteHeader(401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts.Add(1)
					if mode == "dispatch_revoked_after_marker" {
						if err := db.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", common.TokenStatusDisabled).Error; err != nil {
							t.Error(err)
						}
					}

					if mode == "rejected" || mode == "malicious_rejected" || mode == "endpoint_rejected" || mode == "oversized_rejected" {
						w.Header().Set("X-Request-Id", "req-123")
						w.WriteHeader(400)
						if mode == "endpoint_rejected" || mode == "oversized_rejected" {
							body := `{"error":{"code":400,"message":"Model 'openai/gpt-4.1-nano:batch' does not have a :batch endpoint."}}`
							if mode == "oversized_rejected" {
								// A valid JSON prefix padded past the reader limit must not be classified.
								body += strings.Repeat(" ", 16<<10)
							}
							_, _ = w.Write([]byte(body))
							return
						}
						if mode == "malicious_rejected" {
							_, _ = w.Write([]byte(`{"error":{"code":"sk-private-key","message":"Bearer sk-private-key prompt confidential"},"request_id":"sk-private-key"}`))
							return
						}
						_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"specific upstream rejection"}}`))
						return
					}
					if mode == "ambiguous" {
						w.WriteHeader(502)
						return
					}
					var wire struct {
						Endpoint string         `json:"endpoint"`
						Model    string         `json:"model"`
						Provider map[string]any `json:"provider"`
						Requests []batch.Item   `json:"requests"`
					}
					if err := common.DecodeJson(r.Body, &wire); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					assert.Equal(t, "/api/v1/batches", r.URL.Path)
					assert.Equal(t, "/v1/chat/completions", wire.Endpoint)
					assert.Equal(t, "test-model", wire.Model)
					if mode == "provider_routing" {
						assert.Equal(t, map[string]any{"only": []any{"openai"}, "allow_fallbacks": false}, wire.Provider)
					} else {
						assert.Nil(t, wire.Provider)
					}
					if assert.Len(t, wire.Requests, 1) {
						assert.Equal(t, nativeCustomID, wire.Requests[0].CustomID)
						assert.JSONEq(t, `{"max_tokens":8}`, string(wire.Requests[0].Body))
					}
					w.WriteHeader(202)
					_, _ = w.Write([]byte(`{"id":"native-1","status":"validating"}`))
					return
				}
				if strings.HasPrefix(mode, "provider_failed") {
					if polls.Add(1) > 1 {
						_, _ = w.Write([]byte(`{"id":"native-1","status":"failed","results":null,"error":null}`))
						return
					}
					_, _ = w.Write([]byte(`{"id":"native-1","status":"failed","results":null,"error":{"code":"invalid_request","message":"specific upstream rejection"}}`))
					return
				}
				if strings.HasPrefix(mode, "v2_") {
					id, counts, usage := "native-1", `{"total":1,"completed":1,"failed":0}`, `{"cost":0.000004,"prompt_tokens":2,"completion_tokens":1}`
					if mode == "v2_foreign_identity" {
						id = "foreign-1"
					}
					if mode == "v2_null_counts" {
						counts = `{"total":1,"completed":null,"failed":1}`
					}
					if mode == "v2_bad_counts" {
						counts = `{"total":1,"completed":0,"failed":0}`
					}
					if mode == "v2_missing_cost" {
						usage = `{"prompt_tokens":2,"completion_tokens":1}`
					}
					if mode == "v2_withheld" {
						w.WriteHeader(402)
						_, _ = fmt.Fprintf(w, `{"id":%q,"status":"completed","request_counts":%s,"usage":%s,"results":null}`, id, counts, usage)
						return
					}
					_, _ = fmt.Fprintf(w, `{"id":%q,"status":"completed","request_counts":%s,"usage":%s,"results":[{"custom_id":%q,"response":{"status_code":200,"body":{"usage":{"prompt_tokens":2,"completion_tokens":1}}}}]}`, id, counts, usage, nativeCustomID)
					return
				}
				if mode == "aggregate_without_item_cost" {
					_, _ = fmt.Fprintf(w, `{"id":"native-1","status":"completed","usage":{"cost":0.000004},"results":[{"custom_id":%q,"response":{"status_code":200,"body":{"usage":{"prompt_tokens":2,"completion_tokens":1}}}}]}`, nativeCustomID)
					return
				}
				id := "native-1"
				if mode == "foreign_identity" {
					id = "foreign-1"
				}
				_, _ = fmt.Fprintf(w, `{"results":[{"custom_id":%q,"response":{"status_code":200,"body":{"usage":{"prompt_tokens":2,"completion_tokens":1,"cost":0.000004}}}}],"status":"completed","id":%q}`, nativeCustomID, id)
			}))
			t.Cleanup(server.Close)
			base := server.URL + "/api"
			channel := model.Channel{Type: constant.ChannelTypeOpenRouter, Key: "test-key", BaseURL: &base}
			require.NoError(t, db.Create(&channel).Error)
			user := model.User{Username: "batch-user", Password: "unused", Quota: 1000, AuthVersion: 1}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: "batch-token", RemainQuota: 1000}
			require.NoError(t, db.Create(&token).Error)
			tokenID = token.Id
			snapshot := math.BatchBillingSnapshot{EvaluatedAt: time.Now().UTC(), Client: billingexpr.BillingSnapshot{ExprString: `tier("batch", p + c * 2)`, GroupRatio: 1, QuotaPerUnit: 500000}}
			if mode == "aggregate_without_item_cost" {
				snapshot = math.BatchBillingSnapshot{SettlementMode: domain.BatchSettlementOpenRouterCostV1, OpenRouterCost: &common.OpenRouterCostBasis{Version: domain.BatchSettlementOpenRouterCostV1, EffectiveGroupRatio: 1, QuotaPerUnit: 500000}}
			}
			if strings.HasPrefix(mode, "v2_") {
				seedFX(t)
				snapshot, err = openRouterSnapshot()
				require.NoError(t, err)
				require.Equal(t, domain.BatchSettlementOpenRouterJobCostV2, snapshot.SettlementMode)
			}
			if mode == "provider_routing" {
				snapshot.Provider = []byte(`{"only":["openai"],"allow_fallbacks":false}`)
			}
			encoded, err := common.Marshal(snapshot)
			require.NoError(t, err)
			if mode == "provider_failed_settlement_error" || mode == "corrupt_contract" {
				encoded = []byte("{")
			}
			configHash, err := account.BatchChannelConfigHash(&channel)
			require.NoError(t, err)
			job := model.BatchJob{PublicID: "batch_" + mode, UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, ChannelType: channel.Type, CredentialHash: fmt.Sprintf("%x", sha256.Sum256([]byte(channel.Key))), ChannelConfigHash: configHash, Model: "test-model", UpstreamModel: "test-model", Endpoint: "/v1/chat/completions", Status: "validating", BillingStatus: "reserved", SubmitState: "queued", BillingSnapshot: string(encoded), RequestCount: 1, ReservedQuota: 100, ExpiresAt: time.Now().Add(time.Hour).Unix()}
			if mode == "provider_failed_settlement_error" {
				// The durable contract became corrupt after the paid submission.
				job.SubmitState, job.UpstreamID = "submitted", "native-1"
			}
			require.NoError(t, reserve(&job, []model.BatchItem{{CustomID: "client-id", Body: `{"max_tokens":8}`, UpstreamBody: `{"max_tokens":8}`, BillingStatus: "reserved", ReservedQuota: 100}}))
			var item model.BatchItem
			require.NoError(t, db.Where("batch_id = ?", job.ID).First(&item).Error)
			nativeCustomID = item.ProviderCustomID
			require.NotEqual(t, item.CustomID, nativeCustomID)
			if mode == "dispatch_revoked_cache_hold" {
				require.NoError(t, db.First(&token, token.Id).Error)
				token.CacheSchema = 1
				// Complete live hash at the current version, but a corrupt hold.
				token.BatchHeldQuota = 0
				require.NoError(t, common.RedisHSetObj("token:"+common.GenerateHMAC(token.Key), &token, 60))
				require.NoError(t, db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
			}
			if mode == "dispatch_revoked" {
				require.NoError(t, db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
			}
			if mode == "dispatch_expired" {
				require.NoError(t, db.Model(&token).Update("expired_time", common.GetTimestamp()-1).Error)
			}
			if mode == "dispatch_deleted" {
				require.NoError(t, db.Delete(&token).Error)
			}
			if mode == "dispatch_revoked_after_preflight" {
				// Payload is read only after preflight; revoke before the final marker.
				const hook = "batch_test_revoke_before_marker"
				var revoked atomic.Bool
				require.NoError(t, db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
					if tx.Statement.Table == "batch_items" && revoked.CompareAndSwap(false, true) {
						tx.AddError(db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Query().Remove(hook) })
			}

			require.NoError(t, runtime.New().Poll.Execute(context.Background(), "test-worker"))
			require.NoError(t, db.First(&job, job.ID).Error)
			require.NoError(t, runtime.New().Poll.Execute(context.Background(), "test-worker"))
			if mode == "dispatch_revoked_cache_hold" {
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.Zero(t, posts.Load())
				assert.Equal(t, "queued", job.SubmitState)
				assert.EqualValues(t, 100, job.ReservedQuota)
				assert.Zero(t, job.SettledCount, "unsafe holds must not create zero receipts")
				return
			}
			if mode == "dispatch_revoked" || mode == "dispatch_expired" || mode == "dispatch_deleted" || mode == "dispatch_revoked_after_preflight" {
				require.NoError(t, db.First(&job, job.ID).Error)
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Zero(t, posts.Load())
				assert.Zero(t, job.ChargedQuota)
				assert.Equal(t, "rejected", job.SubmitState)
				assert.Equal(t, "settled", job.BillingStatus)
				assert.Zero(t, job.ReservedQuota)
				assert.Zero(t, user.BatchHeldQuota)
				return
			}
			if mode == "corrupt_contract" {
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.Zero(t, posts.Load(), "a corrupt routing contract must fail before a paid POST")
				assert.Equal(t, "queued", job.SubmitState)
				assert.EqualValues(t, 100, job.ReservedQuota)
				assert.Zero(t, job.ChargedQuota)
				return
			}
			if mode == "provider_failed_settlement_error" {
				assert.Zero(t, posts.Load(), "already submitted work must never be replayed")
			} else {
				assert.EqualValues(t, 1, posts.Load(), "ambiguous POST must never be repeated")
			}
			if strings.HasPrefix(mode, "v2_") {
				require.NoError(t, db.First(&job, job.ID).Error)
				require.NoError(t, db.First(&user, user.Id).Error)
				if mode == "v2_aggregate" || mode == "v2_withheld" {
					expected, clamp, err := snapshot.OpenRouterCost.Quota(0.000004)
					require.NoError(t, err)
					require.Nil(t, clamp)
					assert.Equal(t, "settled", job.BillingStatus)
					assert.EqualValues(t, expected, job.ChargedQuota)
					assert.Equal(t, 1000-expected, user.Quota)
					assert.Zero(t, job.ReservedQuota)
					assert.Zero(t, user.BatchHeldQuota)
					assert.NotEmpty(t, job.FinancialDetails)
					if mode == "v2_withheld" {
						assert.Zero(t, job.ResultCount)
					} else {
						assert.Equal(t, 1, job.ResultCount)
					}
				} else {
					assert.Zero(t, job.ChargedQuota)
					assert.EqualValues(t, 100, job.ReservedQuota)
					assert.Equal(t, 1000, user.Quota)
					if mode != "v2_missing_cost" {
						assert.Zero(t, job.ResultCount)
					} else {
						assert.Equal(t, "reconciliation_required", job.BillingStatus)
					}
				}
				return
			}
			if strings.HasPrefix(mode, "provider_failed") || mode == "aggregate_without_item_cost" {
				require.NoError(t, db.First(&job, job.ID).Error)
				if mode == "provider_failed_settlement_error" {
					assert.Equal(t, "reserved", job.BillingStatus)
				} else {
					assert.Equal(t, "reconciliation_required", job.BillingStatus)
				}
				assert.Zero(t, job.ChargedQuota)
				assert.Zero(t, job.SettledCount)
				assert.EqualValues(t, 100, job.ReservedQuota)
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 1000, user.Quota)
				assert.EqualValues(t, 100, user.BatchHeldQuota)
				if strings.HasPrefix(mode, "provider_failed") {
					assert.Equal(t, "failed", job.Status)
					assert.Zero(t, job.ResultCount)
					var diagnostic map[string]any
					require.NoError(t, common.UnmarshalJsonStr(job.LastError, &diagnostic))
					assert.Equal(t, "poll", diagnostic["phase"])
					assert.Equal(t, "invalid_request", diagnostic["code"], "later failed poll without error must retain the diagnostic")
					publicJob, err := common.Marshal(batchdto.Job(domain.Job(job)))
					require.NoError(t, err)
					assert.Contains(t, string(publicJob), `"code":"batch_failed"`)
					assert.NotContains(t, string(publicJob), "specific upstream rejection")
				} else {
					assert.Equal(t, "completed", job.Status)
					assert.Equal(t, 1, job.ResultCount)
					assert.Equal(t, "settled", job.PurchaseStatus)
					assert.Equal(t, "provider_aggregate", job.PurchaseEvidence)
					assert.Equal(t, "0.000004", job.PurchaseUSD)
				}
				return
			}
			if mode == "rejected" || mode == "malicious_rejected" || mode == "endpoint_rejected" || mode == "oversized_rejected" {
				var diagnostic map[string]any
				require.NoError(t, common.UnmarshalJsonStr(job.LastError, &diagnostic))
				assert.EqualValues(t, 1, diagnostic["version"])
				assert.Equal(t, "create", diagnostic["phase"])
				assert.EqualValues(t, 400, diagnostic["http_status"])
				assert.Equal(t, "req-123", diagnostic["request_id"])
				if mode == "endpoint_rejected" {
					assert.Equal(t, "batch_endpoint_unavailable", diagnostic["code"])
					assert.Equal(t, "endpoint_unavailable", diagnostic["category"])
				}
				if mode == "oversized_rejected" {
					assert.Equal(t, "provider_error", diagnostic["code"])
					assert.Equal(t, "unknown", diagnostic["category"])
				}

				assert.NotContains(t, job.LastError, "specific upstream rejection")
				assert.NotContains(t, job.LastError, "sk-private-key")
				assert.NotContains(t, job.LastError, "confidential")
				publicJob, err := common.Marshal(batchdto.Job(domain.Job(job)))
				require.NoError(t, err)
				assert.Contains(t, string(publicJob), `"code":"batch_creation_rejected"`)
				assert.NotContains(t, string(publicJob), "http_status")
				assert.NotContains(t, string(publicJob), "request_id")
				assert.NotContains(t, string(publicJob), "invalid_request")
				assert.Zero(t, job.ResultCount)
				assert.Equal(t, "failed", job.Status)
				assert.Equal(t, "settled", job.BillingStatus)
				assert.Zero(t, job.ChargedQuota)
				assert.Zero(t, job.ReservedQuota)
				return
			}
			if mode == "ambiguous" {
				assert.Equal(t, "submission_unknown", job.SubmitState)
				recovery := recover_batch_identity.NewUsecase(testStore(), gateway.Gateway{}, domain.ChannelKinds{
					OpenAI: constant.ChannelTypeOpenAI, Anthropic: constant.ChannelTypeAnthropic,
					Google: constant.ChannelTypeGemini, OpenRouter: constant.ChannelTypeOpenRouter,
				}, common.GetRandomString)
				_, err := recovery.Execute(context.Background(), recover_batch_identity.Input{PublicID: job.PublicID, UpstreamID: "native-1"})
				require.NoError(t, err)
				require.NoError(t, db.Model(&job).Updates(map[string]any{"lease_owner": "", "lease_until": 0}).Error)
				require.NoError(t, runtime.New().Poll.Execute(context.Background(), "test-worker"))
				require.NoError(t, db.First(&job, job.ID).Error)
			}
			if mode == "foreign_identity" {
				assert.Zero(t, job.ResultCount)
				assert.Zero(t, job.ChargedQuota)
				return
			}
			assert.Equal(t, "settled", job.BillingStatus)
			assert.Equal(t, "settled", job.PurchaseStatus)
			assert.EqualValues(t, 2, job.ChargedQuota)
			assert.Zero(t, job.ReservedQuota)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 998, user.Quota)
			require.NoError(t, db.First(&item, item.ID).Error)
			var result batch.Result
			require.NoError(t, common.UnmarshalJsonStr(item.Result, &result))
			assert.Equal(t, "client-id", result.CustomID)
			var logs []model.Log
			require.NoError(t, db.Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, 2, logs[0].Quota)
			assert.Equal(t, "test-model", logs[0].ModelName)
			assert.EqualValues(t, 1, posts.Load())
		})
	}
}

func reserve(job *model.BatchJob, items []model.BatchItem) error {
	values := make([]domain.Item, len(items))
	for i := range items {
		values[i] = domain.Item(items[i])
	}
	return testStore().CreateBatchReservation((*domain.Job)(job), values)
}
func testStore() storage.Store {
	return storage.NewStore(func() *gorm.DB { return model.DB }, func() *gorm.DB { return model.LOG_DB }, usageadapter.BatchLogs{}, math.BatchMath{}, rediscache.Cache{}, storage.ReadCapabilities{Accounts: accountstorage.Cache(), Tokens: apikeystorage.Reader()})
}

func openRouterSnapshot() (math.BatchBillingSnapshot, error) {
	fx, err := billingapp.NewCosts().FX.Execute(billingdomain.FXBasis{})
	if err != nil {
		return math.BatchBillingSnapshot{}, err
	}
	group := (prices.Prices{}).Group("default", "default")
	effective, err := billingdomain.ApplyBillingFX(group, fx.Factor)
	return math.BatchBillingSnapshot{EvaluatedAt: time.Now().UTC(), SettlementMode: domain.BatchSettlementOpenRouterJobCostV2, OpenRouterCost: &common.OpenRouterCostBasis{Version: domain.BatchSettlementOpenRouterJobCostV2, EffectiveGroupRatio: effective, QuotaPerUnit: common.QuotaPerUnit}, FX: math.StoredFX(fx), PureGroupRatio: group}, err
}
func seedFX(t *testing.T) pricingdomainfx.ModelCostFXSnapshot {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.ModelCostFXRow{}))
	// Synchronize the process cache with the dialect-specific row left by an
	// earlier count iteration before deriving the next optimistic-lock version.
	_ = fxstorage.LoadModelCostFX(context.Background(), pricingdomainfx.ModelCostFXSource)
	var row model.ModelCostFXRow
	err := model.DB.Where("source = ?", pricingdomainfx.ModelCostFXSource).First(&row).Error
	require.True(t, err == nil || errors.Is(err, gorm.ErrRecordNotFound))
	effective := row.EffectiveAt + 1
	if effective <= 1 {
		effective = 100
	}
	fetched := time.Now().Unix() - 60
	snapshot := pricingdomainfx.ModelCostFXSnapshot{Source: pricingdomainfx.ModelCostFXSource, EffectiveAt: effective, FetchedAt: fetched, Rates: map[string]float64{"USD": 90}}
	require.NoError(t, fxstorage.SaveModelCostFX(context.Background(), snapshot, row.Version))
	loaded, err := fxstate.CurrentModelCostFX(pricingdomainfx.ModelCostFXSource)
	require.NoError(t, err)
	return loaded
}

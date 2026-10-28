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
	diagnostics "github.com/QuantumNous/new-api/internal/adapter/batchruntime"
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
	"github.com/QuantumNous/new-api/internal/batch/list_batch_reconciliation"
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
	for _, mode := range []string{"dispatch_revoked_cache_hold", "dispatch_revoked", "dispatch_expired", "dispatch_deleted", "dispatch_revoked_after_preflight", "dispatch_revoked_after_marker", "success", "provider_routing", "corrupt_contract", "ambiguous", "ambiguous_transport", "ambiguous_overdue", "foreign_identity", "rejected", "malicious_rejected", "endpoint_rejected", "oversized_rejected", "provider_failed", "provider_failed_settlement_error", "aggregate_without_item_cost", "v2_aggregate", "v2_foreign_identity", "v2_null_counts", "v2_bad_counts", "v2_missing_cost", "v2_withheld", "v2_window"} {
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
					if mode == "ambiguous" || mode == "ambiguous_overdue" {
						w.WriteHeader(502)
						return
					}
					if mode == "ambiguous_transport" {
						// The connection ends before any provider response is received.
						conn, _, err := w.(http.Hijacker).Hijack()
						if err != nil {
							t.Error(err)
							return
						}
						_ = conn.Close()
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
				if mode == "v2_window" && polls.Add(1) == 1 {
					_, _ = w.Write([]byte(`{"id":"native-1","status":"in_progress","request_counts":{"total":1,"completed":0,"failed":0,"pending":1}}`))
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
			switch mode {
			case "ambiguous_overdue":
				job.CreatedAt = time.Now().Add(-49 * time.Hour).Unix()
			case "v2_window":
				// Older than the provider window plus grace before it is submitted.
				job.CreatedAt = time.Now().Add(-27 * time.Hour).Unix()
			}
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
			if mode == "v2_window" {
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, "submitted", job.SubmitState)
				assert.Equal(t, "in_progress", job.Status)
				assert.Equal(t, "reconciliation_required", job.BillingStatus, "a provider Batch past 24h plus grace needs an operator")
				assert.EqualValues(t, 100, job.ReservedQuota, "the hold stays while paid work may still finish")
				assert.EqualValues(t, 100, user.BatchHeldQuota)
				assert.Equal(t, 1000, user.Quota)
				assert.Equal(t, "provider_window_exceeded", domain.ReconciliationReason((*domain.Job)(&job), time.Now(), testKinds(), math.BatchCodec{}))
				anthropic := job
				anthropic.ChannelType = constant.ChannelTypeAnthropic
				assert.Equal(t, "unsettled_items", domain.ReconciliationReason((*domain.Job)(&anthropic), time.Now(), testKinds(), math.BatchCodec{}), "only 24h completion-window adapters have the provider window")
			}
			if mode == "ambiguous_overdue" {
				// Legacy unknown creations reached production with an erased cause,
				// stored as NULL by older rows.
				require.NoError(t, db.Model(&job).Update("last_error", gorm.Expr("NULL")).Error)
				var nulls int64
				require.NoError(t, db.Model(&model.BatchJob{}).Where("id = ? AND last_error IS NULL", job.ID).Count(&nulls).Error)
				require.EqualValues(t, 1, nulls)
			}
			require.NoError(t, runtime.New().Poll.Execute(context.Background(), "test-worker"))
			if mode == "ambiguous_overdue" {
				require.NoError(t, db.First(&job, job.ID).Error)
				var diagnostic domain.Failure
				require.NoError(t, common.UnmarshalJsonStr(job.LastError, &diagnostic))
				assert.Equal(t, "submission_unknown", diagnostic.Code)
				assert.Equal(t, "ambiguous", diagnostic.Category)
				assert.NotZero(t, diagnostic.AlertedAt)
				assert.Equal(t, "submission_unknown_overdue", domain.ReconciliationReason((*domain.Job)(&job), time.Now(), testKinds(), math.BatchCodec{}))
				diagnostic.AlertedAt = 1
				alerted, err := common.Marshal(diagnostic)
				require.NoError(t, err)
				require.NoError(t, db.Model(&job).Update("last_error", string(alerted)).Error)
				require.NoError(t, runtime.New().Poll.Execute(context.Background(), "test-worker"))
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.JSONEq(t, string(alerted), job.LastError, "the overdue alert is raised once")
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, "submission_unknown", job.SubmitState)
				assert.Equal(t, "reconciliation_required", job.BillingStatus)
				assert.EqualValues(t, 100, user.BatchHeldQuota, "an alert never refunds automatically")
				assert.EqualValues(t, 1, posts.Load())
				return
			}
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
				if mode == "v2_aggregate" || mode == "v2_withheld" || mode == "v2_window" {
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
			if mode == "ambiguous" || mode == "ambiguous_transport" {
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.Equal(t, "submission_unknown", job.SubmitState)
				var diagnostic domain.Failure
				require.NoError(t, common.UnmarshalJsonStr(job.LastError, &diagnostic), "the first creation cause survives later ticks")
				assert.Equal(t, "create", diagnostic.Phase)
				if mode == "ambiguous" {
					assert.Equal(t, "provider_error", diagnostic.Code)
					assert.Equal(t, 502, diagnostic.HTTPStatus)
				} else {
					assert.Equal(t, "transport_error", diagnostic.Code)
					assert.Equal(t, "ambiguous", diagnostic.Category)
					assert.Zero(t, diagnostic.HTTPStatus)
					assert.NotContains(t, job.LastError, "EOF")
				}
				assert.Equal(t, "submission_unknown", domain.ReconciliationReason((*domain.Job)(&job), time.Now(), testKinds(), math.BatchCodec{}))
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

func TestBatchReleaseUnsubmitted(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		submitted  bool
		results    bool
		foreign    bool
		eligible   bool
		// interrupted: the release transition and status are committed, the refund is not.
		interrupted bool
		// halfApplied: only the release transition is committed and the
		// worker's refund attempt then fails.
		halfApplied bool
	}{
		{name: "per-item hold", mode: domain.BatchSettlementOpenRouterCostV1, eligible: true},
		{name: "OpenRouter job cost hold", mode: domain.BatchSettlementOpenRouterJobCostV2, eligible: true},
		{name: "submitted", mode: domain.BatchSettlementOpenRouterCostV1, submitted: true},
		{name: "results received", mode: domain.BatchSettlementOpenRouterCostV1, results: true},
		{name: "OpenRouter job cost with results", mode: domain.BatchSettlementOpenRouterJobCostV2, results: true},
		{name: "foreign lease", mode: domain.BatchSettlementOpenRouterCostV1, foreign: true},
		{name: "interrupted release", mode: domain.BatchSettlementOpenRouterJobCostV2, interrupted: true},
		{name: "half-applied release", mode: domain.BatchSettlementOpenRouterCostV1, halfApplied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldLogDB, oldRedis, oldMemory := model.DB, model.LOG_DB, common.RedisEnabled, common.MemoryCacheEnabled
			model.DB, model.LOG_DB, common.RedisEnabled, common.MemoryCacheEnabled = db, db, false, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB, common.RedisEnabled, common.MemoryCacheEnabled = oldDB, oldLogDB, oldRedis, oldMemory
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.BatchLogEvent{}, &model.Log{}))
			channel := model.Channel{Type: constant.ChannelTypeOpenRouter, Key: "test-key"}
			require.NoError(t, db.Create(&channel).Error)
			user := model.User{Username: "release-user", Password: "unused", Quota: 1000, AuthVersion: 1}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: "release-token", RemainQuota: 1000}
			require.NoError(t, db.Create(&token).Error)
			job := model.BatchJob{PublicID: "batch_release", UserID: user.Id, TokenID: token.Id, ChannelID: channel.Id, ChannelType: channel.Type, Model: "test-model", UpstreamModel: "test-model", Endpoint: "/v1/chat/completions", Status: "validating", BillingStatus: "reserved", SubmitState: "queued", BillingSnapshot: fmt.Sprintf(`{"settlement_mode":%q}`, tc.mode), RequestCount: 1, ReservedQuota: 100, ExpiresAt: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, reserve(&job, []model.BatchItem{{CustomID: "client-id", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 100}}))
			unknown := map[string]any{"submit_state": "submission_unknown", "billing_status": "reconciliation_required", "last_error": `{"version":1,"phase":"create","code":"transport_error","category":"ambiguous","message":"m"}`}
			if tc.submitted {
				unknown["submit_state"], unknown["upstream_id"] = "submitted", "native-1"
			}
			releasedFailure := `{"version":1,"phase":"reconciliation","code":"operator_released_unsubmitted","message":"Operator confirmed no provider Batch exists; hold released."}`
			if tc.interrupted {
				unknown["submit_state"], unknown["status"], unknown["last_error"] = "rejected", "failed", releasedFailure
			}
			if tc.halfApplied {
				unknown["submit_state"], unknown["last_error"] = "rejected", releasedFailure
			}
			require.NoError(t, db.Model(&job).Updates(unknown).Error)
			if tc.results {
				require.NoError(t, testStore().StoreBatchResult(job.ID, "client-id", `{"custom_id":"client-id"}`, "result"))
			}
			entries, _, err := testReconciliation()
			require.NoError(t, err)
			require.Len(t, entries, 1)
			// A foreign lease only blocks this attempt; the Batch itself is releasable.
			assert.Equal(t, tc.eligible || tc.foreign, entries[0].CanRelease, "the operator queue uses the release predicate")
			// Recovery only needs an unknown creation without identity; results do not block it.
			assert.Equal(t, !tc.submitted && !tc.interrupted && !tc.halfApplied, entries[0].CanRecover, "the operator queue uses the recovery predicate")
			require.NotNil(t, entries[0].Diagnostic)
			if tc.interrupted || tc.halfApplied {
				assert.Equal(t, domain.OperatorReleasedCode, entries[0].Diagnostic.Code)
			} else {
				assert.Equal(t, "transport_error", entries[0].Diagnostic.Code)
			}
			if tc.halfApplied {
				const hook = "batch_test_refund_unavailable"
				var failed atomic.Bool
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(hook, func(tx *gorm.DB) {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "batch_jobs" && values["status"] == "failed" && failed.CompareAndSwap(false, true) {
						tx.AddError(errors.New("database unavailable"))
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Update().Remove(hook) })
				require.NoError(t, runtime.New().Poll.Execute(context.Background(), "poll-worker"))
				require.True(t, failed.Load(), "the worker attempted the refund")
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.JSONEq(t, releasedFailure, job.LastError, "a failed retry keeps the operator release marker")
				assert.Empty(t, job.LeaseOwner)
			}
			owner := "release-runner"
			if tc.foreign {
				owner = "poll-worker"
			}
			claimed, err := testStore().ClaimBatchJob(job.ID, owner)
			require.NoError(t, err)
			require.True(t, claimed)
			var events int64
			require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&events).Error)
			if tc.halfApplied {
				result, err := domain.ReleaseUnsubmitted(testStore(), math.BatchCodec{}, diagnostics.Diagnostics{}, (*domain.Job)(&job), "release-runner", 1)
				require.NoError(t, err)
				assert.True(t, result.AlreadyReleased)
				assert.False(t, result.Pending)
				require.NoError(t, db.First(&user, user.Id).Error)
				require.NoError(t, db.First(&token, token.Id).Error)
				assert.Zero(t, user.BatchHeldQuota)
				assert.Zero(t, token.BatchHeldQuota)
				assert.Equal(t, 1000, user.Quota)
				var released int64
				require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&released).Error)
				repeat, err := domain.ReleaseUnsubmitted(testStore(), math.BatchCodec{}, diagnostics.Diagnostics{}, (*domain.Job)(&job), "release-runner", 1)
				require.NoError(t, err)
				assert.True(t, repeat.AlreadyReleased)
				var after int64
				require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&after).Error)
				assert.Equal(t, released, after, "the hold is released once")
				return
			}
			if tc.interrupted {
				// Model MySQL without clientFoundRows: rewriting the same final
				// state reports zero affected rows, which must not block the refund.
				const hook = "batch_test_unchanged_rows"
				require.NoError(t, db.Callback().Update().After("gorm:update").Register(hook, func(tx *gorm.DB) {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "batch_jobs" && values["status"] == "failed" {
						tx.RowsAffected = 0
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Update().Remove(hook) })
				result, err := domain.ReleaseUnsubmitted(testStore(), math.BatchCodec{}, diagnostics.Diagnostics{}, (*domain.Job)(&job), "release-runner", 1)
				require.NoError(t, err)
				assert.True(t, result.AlreadyReleased)
				assert.False(t, result.Pending)
				require.NoError(t, db.First(&user, user.Id).Error)
				require.NoError(t, db.First(&token, token.Id).Error)
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.Zero(t, user.BatchHeldQuota)
				assert.Zero(t, token.BatchHeldQuota)
				assert.Equal(t, 1000, user.Quota)
				assert.Equal(t, "settled", job.BillingStatus)
				assert.Zero(t, job.ReservedQuota)
				assert.JSONEq(t, releasedFailure, job.LastError)
				return
			}

			result, err := domain.ReleaseUnsubmitted(testStore(), math.BatchCodec{}, diagnostics.Diagnostics{}, (*domain.Job)(&job), "release-runner", 1)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			if !tc.eligible {
				if tc.foreign {
					require.ErrorIs(t, err, domain.ErrBatchConflict)
				} else {
					require.ErrorIs(t, err, domain.ErrBatchReleaseIneligible)
				}
				var after int64
				require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&after).Error)
				assert.Equal(t, events, after)
				assert.EqualValues(t, 100, user.BatchHeldQuota)
				assert.EqualValues(t, 100, token.BatchHeldQuota)
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.NotEqual(t, "rejected", job.SubmitState)
				return
			}
			require.NoError(t, err)
			assert.False(t, result.AlreadyReleased)
			assert.False(t, result.Pending)
			assert.EqualValues(t, 100, result.ReleasedQuota)
			assert.Contains(t, result.PreviousLastError, "transport_error")
			assert.Zero(t, user.BatchHeldQuota)
			assert.Zero(t, token.BatchHeldQuota)
			assert.Equal(t, 1000, user.Quota, "release never charges")
			assert.Equal(t, 1000, token.RemainQuota)
			assert.Equal(t, "rejected", job.SubmitState)
			assert.Equal(t, "failed", job.Status)
			assert.Equal(t, "settled", job.BillingStatus)
			assert.Zero(t, job.ReservedQuota)
			assert.Zero(t, job.ChargedQuota)
			assert.Contains(t, job.LastError, domain.OperatorReleasedCode)
			var released []model.BatchQuotaEvent
			require.NoError(t, db.Where("id > ?", 0).Order("id asc").Offset(int(events)).Find(&released).Error)
			require.NotEmpty(t, released)
			for _, event := range released[len(released)-2:] {
				assert.Zero(t, event.HeldQuota, "the final release event returns the hold")
				assert.Zero(t, event.DebitQuota)
			}
			entries, _, err = testReconciliation()
			require.NoError(t, err)
			assert.Empty(t, entries)

			repeat, err := domain.ReleaseUnsubmitted(testStore(), math.BatchCodec{}, diagnostics.Diagnostics{}, (*domain.Job)(&job), "release-runner", 1)
			require.NoError(t, err)
			assert.True(t, repeat.AlreadyReleased)
			assert.Zero(t, repeat.ReleasedQuota)
			var after int64
			require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&after).Error)
			assert.EqualValues(t, int(events)+len(released), after, "a repeated release adds no outbox events")
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Zero(t, user.BatchHeldQuota)
			assert.Equal(t, 1000, user.Quota)
		})
	}
}

func testKinds() domain.ChannelKinds {
	return domain.ChannelKinds{OpenAI: constant.ChannelTypeOpenAI, Anthropic: constant.ChannelTypeAnthropic, Google: constant.ChannelTypeGemini, OpenRouter: constant.ChannelTypeOpenRouter}
}

func testReconciliation() ([]list_batch_reconciliation.Entry, bool, error) {
	return list_batch_reconciliation.NewUsecase(testStore(), gateway.Gateway{}, math.BatchCodec{}, testKinds(), time.Now).Execute(list_batch_reconciliation.Input{Limit: 100})
}

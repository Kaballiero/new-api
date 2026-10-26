package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/batch"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchProviderSnapshotDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
			default:
				driver = sqlite.Open(filepath.Join(t.TempDir(), "provider-snapshot.db"))
			}
			cfg := &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "provider_routing_"}}
			db, err := gorm.Open(driver, cfg)
			require.NoError(t, err)
			require.NoError(t, db.AutoMigrate(&model.BatchJob{}))
			var version string
			query := "SELECT VERSION()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("real %s %s", dialect, version)
			snapshot := BatchBillingSnapshot{Provider: []byte(`{"only":["azure"],"allow_fallbacks":false}`)}
			raw, err := common.Marshal(snapshot)
			require.NoError(t, err)
			job := model.BatchJob{PublicID: "routing-snapshot", UserID: 1, IdempotencyKey: "once", BillingSnapshot: string(raw)}
			require.NoError(t, db.Create(&job).Error)
			legacy := model.BatchJob{PublicID: "legacy-snapshot", UserID: 1, IdempotencyKey: "legacy", BillingSnapshot: `{}`}
			require.NoError(t, db.Create(&legacy).Error)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
			// A new connection models recovery without the admission's in-memory
			// provider value; both old and new JSON contracts must survive startup.
			db, err = gorm.Open(driver, cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&model.BatchJob{}))
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&model.BatchJob{}))
			require.NoError(t, db.First(&job, job.ID).Error)
			var restored BatchBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(job.BillingSnapshot, &restored))
			assert.JSONEq(t, string(snapshot.Provider), string(restored.Provider))
			wire, err := (batch.OpenRouter{}).Create(batch.Submission{Model: "test/model", Endpoint: "/v1/embeddings", Provider: restored.Provider, Items: []batch.Item{{CustomID: "one", Body: []byte(`{"input":"hi"}`)}}})
			require.NoError(t, err)
			assert.Contains(t, string(wire.Body), `"provider":{"only":["azure"],"allow_fallbacks":false},"requests"`)
			require.NoError(t, db.First(&legacy, legacy.ID).Error)
			// Decode into a fresh value as a worker does after restart.
			var oldContract BatchBillingSnapshot
			require.NoError(t, common.UnmarshalJsonStr(legacy.BillingSnapshot, &oldContract))
			assert.Empty(t, oldContract.Provider)
		})
	}
}

func TestBatchUsageRequiresNativeFacts(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, body string
		valid                bool
	}{
		{"cost only", "/v1/chat/completions", `{"usage":{"cost":0.001}}`, false},
		{"total only", "/v1/chat/completions", `{"usage":{"total_tokens":10}}`, false},
		{"cache only", "/v1/messages", `{"usage":{"cache_read_input_tokens":10}}`, false},
		{"explicit zero", "/v1/chat/completions", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, true},
		{"negative", "/v1/chat/completions", `{"usage":{"prompt_tokens":-1,"completion_tokens":0}}`, false},
		{"fractional", "/v1/chat/completions", `{"usage":{"prompt_tokens":1.5,"completion_tokens":0}}`, false},
		{"embedding", "/v1/embeddings", `{"usage":{"prompt_tokens":10,"total_tokens":10}}`, true},
		{"responses", "/v1/responses", `{"usage":{"input_tokens":2,"output_tokens":1}}`, true},
		{"claude", "/v1/messages", `{"usage":{"input_tokens":2,"output_tokens":1}}`, true},
		{"google", "/v1beta/generateContent", `{"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage, err := batchResultUsage(&model.BatchJob{Endpoint: tc.endpoint}, batch.Result{StatusCode: 200, Body: []byte(tc.body)})
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, usage)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestBatchPricingClockIsFrozen(t *testing.T) {
	at := time.Date(2001, 2, 3, 4, 5, 0, 0, time.UTC)
	value, _, err := billingexpr.RunExprWithRequest(`tier("frozen", float(hour("UTC") + minute("UTC") + day("UTC") + month("UTC")))`, billingexpr.TokenParams{}, billingexpr.RequestInput{EvaluatedAt: at})
	require.NoError(t, err)
	assert.Equal(t, 14.0, value)
}

func TestBatchBillingUsesProviderBase(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	oldExpressions := settings.BatchBillingExpr
	oldGroups, oldGroupGroups := ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	general := operation_setting.GetGeneralSetting()
	oldExchange, oldQuota := general.CustomCurrencyExchangeRate, common.QuotaPerUnit
	t.Cleanup(func() {
		settings.BatchBillingExpr = oldExpressions
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroups))
		general.CustomCurrencyExchangeRate, common.QuotaPerUnit = oldExchange, oldQuota
	})
	general.CustomCurrencyExchangeRate, common.QuotaPerUnit = 100, 500000
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":2}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	seedModelCostFXServiceTest(t) // USD=90, so the frozen FX factor is 0.9.
	settings.BatchBillingExpr = map[string]string{"provider-base": `tier("batch", p * 1 + c * 2)`}
	snapshot, err := CaptureBatchBilling("provider-base", "default", "default", nil)
	require.NoError(t, err)
	orSnapshot, err := CaptureOpenRouterBatchBilling("no-display-expression", "default", "default", nil)
	require.NoError(t, err)
	require.NotNil(t, orSnapshot.OpenRouterCost)
	assert.Equal(t, BatchSettlementOpenRouterJobCostV2, orSnapshot.SettlementMode)
	assert.InDelta(t, 1.8, orSnapshot.OpenRouterCost.EffectiveGroupRatio, 1e-12)
	assert.Empty(t, orSnapshot.Client.ExprString, "display availability does not gate OR admission")
	settings.BatchBillingExpr["provider-base"] = `tier("changed", p * 100)`
	result := batch.Result{StatusCode: 200, Body: []byte(`{"usage":{"prompt_tokens":200,"completion_tokens":50}}`)}
	job := &model.BatchJob{ChannelType: constant.ChannelTypeOpenAI, Endpoint: "/v1/chat/completions"}
	usage, err := batchResultUsage(job, result)
	require.NoError(t, err)
	purchase := batchPurchaseUSD(job, result, usage, snapshot, []byte(`{}`))
	require.NotNil(t, purchase)
	assert.InDelta(t, 0.0003, *purchase, 1e-12, "procurement excludes group and FX")
	params := BuildTieredTokenParams(usage, false, billingexpr.UsedVars(snapshot.Client.ExprString))
	charge, err := billingexpr.ComputeTieredQuotaWithRequest(&snapshot.Client, params, billingexpr.RequestInput{EvaluatedAt: snapshot.EvaluatedAt})
	require.NoError(t, err)
	assert.Equal(t, 270, charge.ActualQuotaAfterGroup, "same base times group=2 and FX=0.9")
	snapshot.PurchaseExpression = `tier("legacy", p * 0.5 + c * 1)`
	purchase = batchPurchaseUSD(job, result, usage, snapshot, []byte(`{}`))
	require.NotNil(t, purchase)
	assert.InDelta(t, 0.00015, *purchase, 1e-12, "existing batches retain their frozen procurement basis")
	snapshot.PurchaseExpression = ""
	assert.Nil(t, batchPurchaseUSD(job, result, usage, snapshot, []byte(`{}`)), "missing legacy procurement must not be reinterpreted")
}

func TestBatchWorkerLifecycle(t *testing.T) {
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
			snapshot := BatchBillingSnapshot{EvaluatedAt: time.Now().UTC(), Client: billingexpr.BillingSnapshot{ExprString: `tier("batch", p + c * 2)`, GroupRatio: 1, QuotaPerUnit: 500000}}
			if mode == "aggregate_without_item_cost" {
				snapshot = BatchBillingSnapshot{SettlementMode: BatchSettlementOpenRouterCostV1, OpenRouterCost: &common.OpenRouterCostBasis{Version: BatchSettlementOpenRouterCostV1, EffectiveGroupRatio: 1, QuotaPerUnit: 500000}}
			}
			if strings.HasPrefix(mode, "v2_") {
				seedModelCostFXServiceTest(t)
				snapshot, err = CaptureOpenRouterBatchBilling("test-model", "default", "default", nil)
				require.NoError(t, err)
				require.Equal(t, BatchSettlementOpenRouterJobCostV2, snapshot.SettlementMode)
			}
			if mode == "provider_routing" {
				snapshot.Provider = []byte(`{"only":["openai"],"allow_fallbacks":false}`)
			}
			encoded, err := common.Marshal(snapshot)
			require.NoError(t, err)
			if mode == "provider_failed_settlement_error" || mode == "corrupt_contract" {
				encoded = []byte("{")
			}
			configHash, err := BatchChannelConfigHash(&channel)
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
			require.NoError(t, model.CreateBatchReservation(&job, []model.BatchItem{{CustomID: "client-id", Body: `{"max_tokens":8}`, UpstreamBody: `{"max_tokens":8}`, BillingStatus: "reserved", ReservedQuota: 100}}))
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

			require.NoError(t, RunBatchPollingOnce(context.Background(), "test-worker"))
			require.NoError(t, db.First(&job, job.ID).Error)
			if mode == "v2_window" {
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, "submitted", job.SubmitState)
				assert.Equal(t, "in_progress", job.Status)
				assert.Equal(t, "reconciliation_required", job.BillingStatus, "a provider Batch past 24h plus grace needs an operator")
				assert.EqualValues(t, 100, job.ReservedQuota, "the hold stays while paid work may still finish")
				assert.EqualValues(t, 100, user.BatchHeldQuota)
				assert.Equal(t, 1000, user.Quota)
				assert.Equal(t, "provider_window_exceeded", BatchReconciliationReason(&job, time.Now()))
				anthropic := job
				anthropic.ChannelType = constant.ChannelTypeAnthropic
				assert.Equal(t, "unsettled_items", BatchReconciliationReason(&anthropic, time.Now()), "only 24h completion-window adapters have the provider window")
			}
			if mode == "ambiguous_overdue" {
				// Legacy unknown creations reached production with an erased cause,
				// stored as NULL by older rows.
				require.NoError(t, db.Model(&job).Update("last_error", gorm.Expr("NULL")).Error)
				var nulls int64
				require.NoError(t, db.Model(&model.BatchJob{}).Where("id = ? AND last_error IS NULL", job.ID).Count(&nulls).Error)
				require.EqualValues(t, 1, nulls)
			}
			require.NoError(t, RunBatchPollingOnce(context.Background(), "test-worker"))
			if mode == "ambiguous_overdue" {
				require.NoError(t, db.First(&job, job.ID).Error)
				var diagnostic model.BatchFailure
				require.NoError(t, common.UnmarshalJsonStr(job.LastError, &diagnostic))
				assert.Equal(t, "submission_unknown", diagnostic.Code)
				assert.Equal(t, "ambiguous", diagnostic.Category)
				assert.NotZero(t, diagnostic.AlertedAt)
				assert.Equal(t, "submission_unknown_overdue", BatchReconciliationReason(&job, time.Now()))
				diagnostic.AlertedAt = 1
				alerted, err := common.Marshal(diagnostic)
				require.NoError(t, err)
				require.NoError(t, db.Model(&job).Update("last_error", string(alerted)).Error)
				require.NoError(t, RunBatchPollingOnce(context.Background(), "test-worker"))
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
					publicJob, err := common.Marshal(job)
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
				publicJob, err := common.Marshal(job)
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
				var diagnostic model.BatchFailure
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
				assert.Equal(t, "submission_unknown", BatchReconciliationReason(&job, time.Now()))
				claimed, err := model.ClaimBatchJob(job.ID, "recovery")
				require.NoError(t, err)
				require.True(t, claimed)
				require.NoError(t, RecoverBatchIdentity(context.Background(), &job, "native-1", "recovery"))
				require.NoError(t, db.Model(&job).Updates(map[string]any{"lease_owner": "", "lease_until": 0}).Error)
				require.NoError(t, RunBatchPollingOnce(context.Background(), "test-worker"))
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

func TestBatchTokenEstimates(t *testing.T) {
	oldCount := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = oldCount })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/batches", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "different-parent-model")
	snapshot := BatchBillingSnapshot{Client: billingexpr.BillingSnapshot{ModelName: "gpt-4o", ExprString: `tier("tokens", p + c)`, ExprHash: billingexpr.ExprHashString(`tier("tokens", p + c)`), GroupRatio: 1, QuotaPerUnit: 1000000, ExprVersion: 1}}
	for _, tc := range []struct {
		endpoint, body string
		output         int
		request        dto.Request
		framing        int
	}{
		{"/v1/chat/completions", `{"model":"mapped","messages":[{"role":"user","content":"hello world"}],"max_tokens":10,"temperature":0,"unknown":false}`, 10, &dto.GeneralOpenAIRequest{}, 6},
		{"/v1/responses", `{"model":"mapped","input":"hello world","max_output_tokens":10}`, 10, &dto.OpenAIResponsesRequest{}, 0},
		{"/v1/messages", `{"model":"mapped","messages":[{"role":"user","content":"hello world"}],"max_tokens":10}`, 10, &dto.ClaudeRequest{}, 0},
		{"/v1beta/generateContent", `{"contents":[{"parts":[{"text":"hello world"}]}],"generationConfig":{"maxOutputTokens":10}}`, 10, &dto.GeminiChatRequest{}, 0},
		{"/v1/embeddings", `{"model":"mapped","input":"hello world"}`, 0, &dto.EmbeddingRequest{}, 0},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			require.NoError(t, common.UnmarshalJsonStr(tc.body, tc.request))
			want := CountTextToken(tc.request.GetTokenCountMeta().CombineText, "gpt-4o") + tc.framing + tc.output
			for _, raw := range []string{tc.body, " \n" + strings.TrimSuffix(tc.body, "}") + `,"opaque_provider_extension":{"ignored":"` + strings.Repeat("x", 1000) + `"}}`} {
				body := []byte(raw)
				quota, err := EstimateBatchItem(c, snapshot, tc.endpoint, body, tc.output)
				require.NoError(t, err)
				assert.Equal(t, want, quota)
				assert.Equal(t, raw, string(body), "counting preserves raw provider fields")
			}
		})
	}
	body, err := relaycommon.ApplyParamOverride([]byte(`{"model":"mapped","messages":[{"role":"user","content":"original"}],"max_tokens":10,"temperature":0,"opaque":false}`), map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello world"}}}, nil)
	require.NoError(t, err)
	quota, err := EstimateBatchItem(c, snapshot, "/v1/chat/completions", body, 10)
	require.NoError(t, err)
	var prepared dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(body, &prepared))
	assert.Equal(t, CountTextToken(prepared.GetTokenCountMeta().CombineText, "gpt-4o")+16, quota, "estimate uses prepared overrides")
	assert.Contains(t, string(body), `"temperature":0`)
	assert.Contains(t, string(body), `"opaque":false`)
	assert.Contains(t, string(body), `"model":"mapped"`)
	assert.Equal(t, "different-parent-model", common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
}

func TestBatchCanonicalPurchaseUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldRedis, oldMemory := model.DB, common.RedisEnabled, common.MemoryCacheEnabled
	model.DB, common.RedisEnabled, common.MemoryCacheEnabled = db, false, false
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.MemoryCacheEnabled = oldDB, oldRedis, oldMemory
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.BatchLogEvent{}))
	for _, tc := range []struct {
		endpoint, body, expression string
		cost                       float64
	}{
		{"/v1/chat/completions", `{"usage":{"prompt_tokens":1000,"completion_tokens":100,"prompt_tokens_details":{"cached_tokens":800,"image_tokens":50,"audio_tokens":20},"completion_tokens_details":{"audio_tokens":30,"image_tokens":10}}}`, `len >= 1000 ? tier("full",p*2+cr*0.2+img*3+ai*4+c*5+ao*6+img_o*7) : tier("wrong",0)`, 1200},
		{"/v1/responses", `{"usage":{"input_tokens":1000,"output_tokens":10,"input_tokens_details":{"cached_tokens":800}}}`, `len >= 1000 ? tier("full",p*2+cr*0.2+c*3) : tier("wrong",0)`, 590},
		{"/v1/messages", `{"usage":{"input_tokens":100,"output_tokens":10,"cache_read_input_tokens":800,"cache_creation_input_tokens":300,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":200}}}`, `len >= 1200 ? tier("full",p*2+cr*0.2+cc*3+cc1h*4+c*5) : tier("wrong",0)`, 1510},
	} {
		t.Run(tc.endpoint, func(t *testing.T) {
			job := &model.BatchJob{Endpoint: tc.endpoint, ChannelType: constant.ChannelTypeOpenAI}
			result := batch.Result{StatusCode: 200, Body: []byte(tc.body)}
			usage, err := batchResultUsage(job, result)
			require.NoError(t, err)
			snapshot := BatchBillingSnapshot{PurchaseExpression: tc.expression, Client: billingexpr.BillingSnapshot{ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression), GroupRatio: 2, QuotaPerUnit: 1000000, ExprVersion: 1}}
			purchase := batchPurchaseUSD(job, result, usage, snapshot, []byte(`{}`))
			require.NotNil(t, purchase)
			assert.InDelta(t, tc.cost/1000000, *purchase, 1e-12)
			user := model.User{Username: "canonical-" + tc.endpoint, AffCode: tc.endpoint, Password: "unused", Quota: 100}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: "canonical-" + tc.endpoint, RemainQuota: 100}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Type: constant.ChannelTypeOpenAI, Key: "unused"}
			require.NoError(t, db.Create(&channel).Error)
			encoded, err := common.Marshal(snapshot)
			require.NoError(t, err)
			job.PublicID = "canonical-" + tc.endpoint
			job.UserID = user.Id
			job.TokenID = token.Id
			job.ChannelID = channel.Id
			job.RequestCount = 3
			job.ReservedQuota = 30
			job.BillingSnapshot = string(encoded)
			require.NoError(t, model.CreateBatchReservation(job, []model.BatchItem{
				{CustomID: "missing", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
				{CustomID: "ready", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
				{CustomID: "next", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
			}))
			missing, err := common.Marshal(batch.Result{StatusCode: 200, Body: []byte(`{}`)})
			require.NoError(t, err)
			ready, err := common.Marshal(result)
			require.NoError(t, err)
			require.NoError(t, model.StoreBatchResult(job.ID, "missing", string(missing), "missing"))
			require.NoError(t, model.StoreBatchResult(job.ID, "ready", string(ready), "ready"))
			require.NoError(t, model.StoreBatchResult(job.ID, "next", string(ready), "next"))
			require.NoError(t, SettleBatchResults(job))
			require.NoError(t, SettleBatchResults(job))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 100-int(tc.cost*4), user.Quota, "both ready items settle despite debt and earlier missing usage")
			var item model.BatchItem
			require.NoError(t, db.Where("batch_id = ? AND custom_id = ?", job.ID, "ready").First(&item).Error)
			assert.EqualValues(t, int(tc.cost*2), item.ChargedQuota)
			var details BatchFinancialDetails
			require.NoError(t, common.UnmarshalJsonStr(item.FinancialDetails, &details))
			require.NotNil(t, details.PurchaseUSD)
			assert.InDelta(t, tc.cost/1000000, *details.PurchaseUSD, 1e-12)
			assert.Contains(t, item.FinancialDetails, `"matched_tier":"full"`)
			var missingItem model.BatchItem
			require.NoError(t, db.Where("batch_id = ? AND custom_id = ?", job.ID, "missing").First(&missingItem).Error)
			assert.Equal(t, "reconciliation_required", missingItem.BillingStatus)

		})
	}
}

func TestBatchOpenRouterActualCost(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.BatchJob{}, &model.BatchItem{}, &model.BatchQuotaEvent{}, &model.BatchLogEvent{}))
	for _, tc := range []struct {
		name, body        string
		want              int
		settled, unbilled bool
	}{
		{name: "cost only higher", body: `{"usage":{"cost":0.00004,"is_byok":true,"cost_details":{"upstream_inference_cost":9}}}`, want: 80, settled: true},
		{name: "cost only lower", body: `{"usage":{"cost":0.00001}}`, want: 20, settled: true},
		{name: "known zero", body: `{"usage":{"cost":0}}`, settled: true},
		{name: "missing individual cost", body: `{"usage":{"prompt_tokens":10,"completion_tokens":3}}`, want: 50},
		{name: "null", body: `{"usage":{"cost":null}}`, want: 50},
		{name: "string", body: `{"usage":{"cost":"0"}}`, want: 50},
		{name: "negative", body: `{"usage":{"cost":-1}}`, want: 50},
		{name: "proven unbilled", body: `{}`, settled: true, unbilled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			t.Cleanup(func() {
				for _, table := range []string{"batch_log_events", "batch_quota_events", "batch_items", "batch_jobs"} {
					require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
				}
			})
			seedUser(t, 835, 10000)
			seedToken(t, 835, 835, "or-batch-cost", 10000)
			seedChannel(t, 835)
			basis := common.OpenRouterCostBasis{Version: BatchSettlementOpenRouterCostV1, EffectiveGroupRatio: 4, QuotaPerUnit: 500000, EstimatedUSD: 0.000025}
			encoded, err := common.Marshal(BatchBillingSnapshot{SettlementMode: BatchSettlementOpenRouterCostV1, OpenRouterCost: &basis})
			require.NoError(t, err)
			job := &model.BatchJob{PublicID: "or-cost-test", UserID: 835, TokenID: 835, ChannelID: 835, ChannelType: constant.ChannelTypeOpenRouter, Model: "or/model", Endpoint: "/v1/chat/completions", BillingSnapshot: string(encoded), RequestCount: 1, ReservedQuota: 50, UpstreamUsage: `{"cost":99}`}
			require.NoError(t, model.CreateBatchReservation(job, []model.BatchItem{{CustomID: "item", Body: `{}`, UpstreamBody: `{}`, ReservedQuota: 50, BillingStatus: "reserved"}}))
			result, err := common.Marshal(batch.Result{CustomID: "item", StatusCode: 200, Body: []byte(tc.body), Unbilled: tc.unbilled})
			require.NoError(t, err)
			require.NoError(t, model.StoreBatchResult(job.ID, "item", string(result), "result"))
			require.NoError(t, SettleBatchResults(job))
			require.NoError(t, SettleBatchResults(job))
			var item model.BatchItem
			require.NoError(t, model.DB.Where("batch_id = ?", job.ID).First(&item).Error)
			var user model.User
			require.NoError(t, model.DB.First(&user, 835).Error)
			var token model.Token
			require.NoError(t, model.DB.First(&token, 835).Error)
			if tc.settled {
				assert.Equal(t, "settled", item.BillingStatus)
				assert.EqualValues(t, tc.want, item.ChargedQuota)
				assert.Equal(t, 10000-tc.want, user.Quota)
				assert.Equal(t, 10000-tc.want, token.RemainQuota)
				if !tc.unbilled {
					assert.Contains(t, item.FinancialDetails, BatchSettlementOpenRouterCostV1)
					assert.NotContains(t, item.FinancialDetails, "matched_tier")
				}
			} else {
				assert.Equal(t, "reconciliation_required", item.BillingStatus)
				assert.Zero(t, item.ChargedQuota)
				assert.Equal(t, 10000, user.Quota, "aggregate cost never allocates to unknown item")
				assert.Equal(t, 10000, token.RemainQuota)
				assert.EqualValues(t, 50, user.BatchHeldQuota)
				assert.EqualValues(t, 50, token.BatchHeldQuota)
			}
		})
	}
}

func TestBatchFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, requestID, wantCode, wantRequestID string
	}{
		{"known classification", `{"error":{"code":"model_not_found","message":"private prompt Bearer sk-secret"}}`, "req-123", "model_not_found", "req-123"},
		{"numeric envelope", `{"error":{"code":400,"message":"private prompt Bearer sk-secret"}}`, "", "400", ""},
		{"numeric snapshot", `{"code":402,"message":"private prompt Bearer sk-secret"}`, "", "402", ""},
		{"out of range numeric", `{"error":{"code":999}}`, "", "provider_error", ""},
		{"boolean code", `{"error":{"code":true}}`, "", "provider_error", ""},
		{"unknown classification", `{"error":{"code":"sk-secret","message":"private prompt"}}`, "Bearer sk-secret", "provider_error", ""},
		{"secret shaped request ID", `{"error":{"code":"invalid_request"}}`, "req-sk-secret", "invalid_request", ""},
		{"malformed JSON", `private prompt Bearer sk-secret`, "", "provider_error", ""},
		{"oversized body", `{"error":{"code":"model_not_found"},"message":"` + strings.Repeat("x", 16<<10) + `private prompt"}`, strings.Repeat("x", 129), "provider_error", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := normalizedBatchFailure("create", 400, tc.requestID, []byte(tc.body))
			assert.Less(t, len(stored), 4096)
			assert.NotContains(t, stored, "private prompt")
			assert.NotContains(t, stored, "sk-secret")
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr(stored, &diagnostic))
			assert.Equal(t, tc.wantCode, diagnostic.Code)
			assert.Equal(t, tc.wantRequestID, diagnostic.RequestID)
			job := model.BatchJob{PublicID: "batch_test", Status: "failed", LastError: stored}
			for _, response := range []any{job, &job, gin.H{"data": []model.BatchJob{job}}} {
				data, err := common.Marshal(response)
				require.NoError(t, err)
				assert.Contains(t, string(data), `"code":"batch_creation_rejected"`)
				assert.NotContains(t, string(data), `"code":"`+tc.wantCode+`"`)
				assert.NotContains(t, string(data), "request_id")
				assert.NotContains(t, string(data), "http_status")
			}
		})
	}
	// These six real rejection templates previously collapsed to numeric HTTP 400.
	for _, name := range []string{"openai/gpt-4-turbo", "openai/gpt-4.1-mini", "openai/gpt-4.1-nano", "openai/gpt-5.4-pro", "openai/gpt-5.5-pro", "openai/gpt-5.5"} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":400,"message":"Model '%s:batch' does not have a :batch endpoint."}}`, name)
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr(normalizedBatchFailure("create", 400, "req-123", []byte(body)), &diagnostic))
			assert.Equal(t, "batch_endpoint_unavailable", diagnostic.Code)
			assert.Equal(t, "endpoint_unavailable", diagnostic.Category)
			assert.Equal(t, "The provider reported that the requested model has no Batch endpoint.", diagnostic.Message)
			assert.NotContains(t, diagnostic.Message, name)
		})
	}
	for _, tc := range []struct {
		name, body, code, category string
	}{
		{"unknown 400 is not capacity", `{"error":{"code":400,"message":"unrecognized rejection"}}`, "400", "unknown"},
		{"typed capacity", `{"error":{"code":400,"metadata":{"error_type":"provider_overloaded","raw":"Bearer sk-secret"}}}`, "provider_overloaded", "capacity"},
		{"typed parameters", `{"error":{"code":400,"metadata":{"error_type":"context_length_exceeded"}}}`, "context_length_exceeded", "invalid_request"},
		{"typed access", `{"error":{"code":403,"metadata":{"error_type":"permission_denied","raw":"private prompt"}}}`, "permission_denied", "access"},
		{"anthropic access", `{"type":"error","error":{"type":"permission_error","message":"private prompt"}}`, "permission_error", "access"},
		{"google parameters", `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"private prompt"}}`, "INVALID_ARGUMENT", "invalid_request"},
		{"resource limit is not capacity", `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, "RESOURCE_EXHAUSTED", "rate_limit"},
		{"snapshot endpoint", `{"code":400,"message":"Model 'openai/gpt-4o:batch' does not have a :batch endpoint."}`, "batch_endpoint_unavailable", "endpoint_unavailable"},
		{"no substring guessing", `{"error":{"code":400,"message":"private prompt says Model 'openai/gpt-4o:batch' does not have a :batch endpoint."}}`, "400", "unknown"},
		{"secret in matched model discarded", `{"error":{"code":400,"message":"Model 'sk-secret:batch' does not have a :batch endpoint."}}`, "batch_endpoint_unavailable", "endpoint_unavailable"},
		{"URL and raw secrets discarded", `{"error":{"code":400,"message":"https://example.test?token=secret-token","metadata":{"error_type":"sk-secret","raw":{"Authorization":"Bearer sk-secret","prompt":"private prompt"}}}}`, "400", "unknown"},
		{"outer reason not inherited", `{"metadata":{"error_type":"provider_overloaded"},"error":{"code":400}}`, "400", "unknown"},
		{"malformed nested error", `{"error":"Bearer sk-secret"}`, "provider_error", "unknown"},
		{"oversized valid JSON", `{"error":{"code":400,"message":"Model 'openai/gpt-4o:batch' does not have a :batch endpoint."},"padding":"` + strings.Repeat("x", 16<<10) + `"}`, "provider_error", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := normalizedBatchFailure("create", 400, "req-123", []byte(tc.body))
			var diagnostic model.BatchFailure
			require.NoError(t, common.UnmarshalJsonStr(stored, &diagnostic))
			assert.Equal(t, tc.code, diagnostic.Code)
			assert.Equal(t, tc.category, diagnostic.Category)
			assert.Less(t, len(stored), 4096)
			for _, secret := range []string{"sk-secret", "private prompt", "secret-token", "Authorization", "https://"} {
				assert.NotContains(t, stored, secret)
			}
			data, err := common.Marshal(model.BatchJob{Status: "failed", LastError: stored})
			require.NoError(t, err)
			assert.Contains(t, string(data), `"code":"batch_creation_rejected"`)
			assert.NotContains(t, string(data), "category")
			assert.NotContains(t, string(data), "req-123")
		})
	}

	for _, tc := range []struct {
		status, lastError string
		failed            bool
	}{
		{"completed", "internal transport Bearer sk-secret", false},
		{"validating", "internal transport Bearer sk-secret", false},
		{"failed", "internal transport Bearer sk-secret", true},
		{"failed", `{"version":99,"phase":"create","http_status":400,"message":"sk-secret"}`, true},
		{"failed", `{"version":1,"phase":"create","http_status":400,"message":"sk-secret"`, true},
	} {
		data, err := common.Marshal(model.BatchJob{Status: tc.status, LastError: tc.lastError})
		require.NoError(t, err)
		assert.NotContains(t, string(data), "sk-secret")
		assert.NotContains(t, string(data), "internal transport")
		if tc.failed {
			assert.Contains(t, string(data), `"code":"batch_failed"`)
		} else {
			assert.NotContains(t, string(data), `"error"`)
		}
	}
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
		{name: "per-item hold", mode: BatchSettlementOpenRouterCostV1, eligible: true},
		{name: "OpenRouter job cost hold", mode: BatchSettlementOpenRouterJobCostV2, eligible: true},
		{name: "submitted", mode: BatchSettlementOpenRouterCostV1, submitted: true},
		{name: "results received", mode: BatchSettlementOpenRouterCostV1, results: true},
		{name: "OpenRouter job cost with results", mode: BatchSettlementOpenRouterJobCostV2, results: true},
		{name: "foreign lease", mode: BatchSettlementOpenRouterCostV1, foreign: true},
		{name: "interrupted release", mode: BatchSettlementOpenRouterJobCostV2, interrupted: true},
		{name: "half-applied release", mode: BatchSettlementOpenRouterCostV1, halfApplied: true},
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
			require.NoError(t, model.CreateBatchReservation(&job, []model.BatchItem{{CustomID: "client-id", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 100}}))
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
				require.NoError(t, model.StoreBatchResult(job.ID, "client-id", `{"custom_id":"client-id"}`, "result"))
			}
			entries, _, err := ListBatchReconciliation("", 100)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			// A foreign lease only blocks this attempt; the Batch itself is releasable.
			assert.Equal(t, tc.eligible || tc.foreign, entries[0].CanRelease, "the operator queue uses the release predicate")
			// Recovery only needs an unknown creation without identity; results do not block it.
			assert.Equal(t, !tc.submitted && !tc.interrupted && !tc.halfApplied, entries[0].CanRecover, "the operator queue uses the recovery predicate")
			require.NotNil(t, entries[0].Diagnostic)
			if tc.interrupted || tc.halfApplied {
				assert.Equal(t, batchOperatorReleasedCode, entries[0].Diagnostic.Code)
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
				require.NoError(t, RunBatchPollingOnce(context.Background(), "poll-worker"))
				require.True(t, failed.Load(), "the worker attempted the refund")
				require.NoError(t, db.First(&job, job.ID).Error)
				assert.JSONEq(t, releasedFailure, job.LastError, "a failed retry keeps the operator release marker")
				assert.Empty(t, job.LeaseOwner)
			}
			owner := "release-runner"
			if tc.foreign {
				owner = "poll-worker"
			}
			claimed, err := model.ClaimBatchJob(job.ID, owner)
			require.NoError(t, err)
			require.True(t, claimed)
			var events int64
			require.NoError(t, db.Model(&model.BatchQuotaEvent{}).Count(&events).Error)
			if tc.halfApplied {
				result, err := ReleaseUnsubmittedBatch(&job, "release-runner", 1)
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
				repeat, err := ReleaseUnsubmittedBatch(&job, "release-runner", 1)
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
				result, err := ReleaseUnsubmittedBatch(&job, "release-runner", 1)
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

			result, err := ReleaseUnsubmittedBatch(&job, "release-runner", 1)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			if !tc.eligible {
				if tc.foreign {
					require.ErrorIs(t, err, model.ErrBatchConflict)
				} else {
					require.ErrorIs(t, err, ErrBatchReleaseIneligible)
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
			assert.Contains(t, job.LastError, batchOperatorReleasedCode)
			var released []model.BatchQuotaEvent
			require.NoError(t, db.Where("id > ?", 0).Order("id asc").Offset(int(events)).Find(&released).Error)
			require.NotEmpty(t, released)
			for _, event := range released[len(released)-2:] {
				assert.Zero(t, event.HeldQuota, "the final release event returns the hold")
				assert.Zero(t, event.DebitQuota)
			}
			entries, _, err = ListBatchReconciliation("", 100)
			require.NoError(t, err)
			assert.Empty(t, entries)

			repeat, err := ReleaseUnsubmittedBatch(&job, "release-runner", 1)
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

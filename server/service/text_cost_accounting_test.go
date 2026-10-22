package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Each PostgreSQL matrix subtest owns a schema; supplied DSNs require CREATE SCHEMA.
func TestFixedPriceBillingDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_FIXED_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_FIXED_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = isolatedCostPostgres(t, dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(dialect.name, dialect.name)
			t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB; common.SetDatabaseTypes(oldMainType, oldLogType) })
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
			versionQuery := "select version()"
			if dialect.name == common.DatabaseTypeSQLite {
				versionQuery = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database: %s", version)
			runFixedPriceAccountingCases(t, db)
		})
	}
}

func TestTextDeliveryOutcomeDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_FIXED_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_FIXED_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = isolatedCostPostgres(t, dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(dialect.name, dialect.name)
			t.Cleanup(func() { model.DB, model.LOG_DB = oldDB, oldLogDB; common.SetDatabaseTypes(oldMainType, oldLogType) })
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
			versionQuery := "select version()"
			if dialect.name == common.DatabaseTypeSQLite {
				versionQuery = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database: %s", version)
			runTextDeliveryOutcomeCases(t, db)
		})
	}
}

func runTextDeliveryOutcomeCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	oldErrorLogEnabled, oldConsumeLogEnabled := constant.ErrorLogEnabled, common.LogConsumeEnabled
	constant.ErrorLogEnabled, common.LogConsumeEnabled = true, true
	t.Cleanup(func() { constant.ErrorLogEnabled, common.LogConsumeEnabled = oldErrorLogEnabled, oldConsumeLogEnabled })

	for index, tc := range []struct {
		name            string
		streamStatus    *relaycommon.StreamStatus
		cancelRequest   bool
		errorLogEnabled bool
		maskedError     bool
		wantReason      string
	}{
		{name: "successful done", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			return status
		}()},
		{name: "successful EOF", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
			return status
		}()},
		{name: "clean handler stop", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, nil)
			return status
		}()},
		{name: "timeout", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			return status
		}(), wantReason: "stream_timeout"},
		{name: "client gone", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("client connection closed"))
			return status
		}(), wantReason: "stream_client_gone"},
		{name: "scanner error", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonScannerErr, errors.New("scanner failed"))
			return status
		}(), wantReason: "stream_scanner_error"},
		{name: "soft error after done", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			status.RecordError("upstream error")
			return status
		}(), wantReason: "stream_error"},
		{name: "soft error after EOF", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
			status.RecordError("upstream error")
			return status
		}(), wantReason: "stream_error"},
		{name: "masked soft error", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, nil)
			status.RecordError(common.MaskSensitiveInfo("upstream api_key:credential"))
			return status
		}(), maskedError: true, wantReason: "stream_error"},
		{name: "canceled nonstream", cancelRequest: true, wantReason: "context_canceled"},
		{name: "error logging disabled", streamStatus: func() *relaycommon.StreamStatus {
			status := relaycommon.NewStreamStatus()
			status.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("client connection closed"))
			return status
		}(), errorLogEnabled: false, wantReason: "stream_client_gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			constant.ErrorLogEnabled = tc.errorLogEnabled || tc.name != "error logging disabled"
			user := model.User{Username: fmt.Sprintf("delivery_outcome_%d", index), Quota: 1_000, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("delivery-outcome-%d", index), Name: "delivery-outcome", RemainQuota: 1_000, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Name: "delivery-outcome", Key: "unused", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("user_id = ?", user.Id).Delete(&model.Log{}).Error)
				require.NoError(t, db.Unscoped().Delete(&token).Error)
				require.NoError(t, db.Unscoped().Delete(&user).Error)
				require.NoError(t, db.Unscoped().Delete(&channel).Error)
			})

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx.Set(common.RequestIdKey, fmt.Sprintf("delivery-request-%d", index))
			info := &relaycommon.RelayInfo{
				UserId: user.Id, TokenId: token.Id, TokenKey: token.Key,
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channel.Id},
				OriginModelName: "delivery-test", UsingGroup: "default", StartTime: time.Now(),
				IsStream: tc.streamStatus != nil, RelayFormat: types.RelayFormatOpenAI,
				UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
				PriceData:    hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
				StreamStatus: tc.streamStatus,
			}
			require.Nil(t, service.PreConsumeBilling(ctx, 5, info))
			if tc.cancelRequest {
				requestContext, cancel := context.WithCancel(ctx.Request.Context())
				ctx.Request = ctx.Request.WithContext(requestContext)
				cancel()
			}

			billinggateway.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5}, nil)

			var logs []model.Log
			require.NoError(t, db.Where("user_id = ?", user.Id).Order("id").Find(&logs).Error)
			require.Len(t, logs, map[bool]int{true: 2, false: 1}[tc.wantReason != "" && constant.ErrorLogEnabled])
			consume := logs[0]
			require.Equal(t, model.LogTypeConsume, consume.Type)
			require.Equal(t, 5, consume.Quota)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(consume.Other, &other))
			if tc.wantReason == "" {
				assert.NotContains(t, other, "delivery_status")
				return
			}
			assert.Equal(t, "failed", other["delivery_status"])
			assert.Equal(t, tc.wantReason, other["delivery_reason"])
			if constant.ErrorLogEnabled {
				errorLog := logs[1]
				require.Equal(t, model.LogTypeError, errorLog.Type)
				assert.Zero(t, errorLog.Quota)
				assert.Equal(t, consume.RequestId, errorLog.RequestId)
				assert.Equal(t, consume.ChannelId, errorLog.ChannelId)
				assert.Equal(t, consume.ModelName, errorLog.ModelName)
				assert.Equal(t, consume.UserId, errorLog.UserId)
			}
			if tc.maskedError {
				for _, log := range logs {
					assert.NotContains(t, log.Other, "credential")
					assert.Contains(t, log.Other, "api_key:***")
				}
			}
		})
	}
}

func runFixedPriceAccountingCases(t *testing.T, db *gorm.DB) {
	t.Helper()
	const mixed = `len <= 32000 ? tier("short", fixed(0.01)) : tier("long", p * 2)`
	const flat = `tier("request", fixed(0.01))`
	const startingQuota = 2_000_000
	operation_setting.SetToolPriceForTest("fixed_billing_tool", 4)
	t.Cleanup(func() { operation_setting.DeleteToolPriceForTest("fixed_billing_tool") })
	for index, tc := range []struct {
		name, expression                          string
		estimate                                  int
		usage                                     *dto.Usage
		audio, stream, refund, insufficient, tool bool
		groupRatio                                float64
		want                                      int
		unit                                      billingexpr.BillingUnit
	}{
		{name: "missing usage charges once", expression: flat, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "zero usage charges once", expression: flat, usage: &dto.Usage{}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "stream charges once", expression: flat, stream: true, usage: &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "audio zero usage charges once", expression: flat, audio: true, usage: &dto.Usage{}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "audio missing usage charges once", expression: flat, audio: true, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "token reservation refunds to fixed price", expression: mixed, estimate: 50000, usage: &dto.Usage{PromptTokens: 100, TotalTokens: 100}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "fixed reservation settles token fallback", expression: mixed, estimate: 100, usage: &dto.Usage{PromptTokens: 50000, TotalTokens: 50000}, want: 50000, unit: billingexpr.BillingUnitToken},
		{name: "missing usage uses estimated token fallback", expression: mixed, estimate: 50000, want: 50000, unit: billingexpr.BillingUnitToken},
		{name: "evaluation error retains fixed reservation metadata", expression: `p == 50 ? tier("error", param("missing") * p) : tier("request", fixed(0.01))`, estimate: 100, usage: &dto.Usage{PromptTokens: 50, TotalTokens: 50}, want: 5000, unit: billingexpr.BillingUnitRequest},
		{name: "explicit zero remains free", expression: `tier("free", fixed(0))`, usage: &dto.Usage{PromptTokens: 100, TotalTokens: 100}, unit: billingexpr.BillingUnitRequest},
		{name: "multipliers and separate tool surcharge", expression: flat + ` * (param("fast") == true ? 2 : 1)`, groupRatio: 1.5, tool: true, want: 18000, unit: billingexpr.BillingUnitRequest},
		{name: "failed request refunds exactly once", expression: flat, refund: true},
		{name: "insufficient wallet never reserves tokens", expression: flat, insufficient: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota := startingQuota
			if tc.insufficient {
				quota = 1
			}
			user := model.User{Username: fmt.Sprintf("fixed_billing_%d", index), Quota: quota, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("fixed-billing-test-%d", index), Name: "fixed-billing", RemainQuota: startingQuota, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			channel := model.Channel{Name: "fixed-billing", Key: "unused", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("user_id = ?", user.Id).Delete(&model.Log{}).Error)
				require.NoError(t, db.Unscoped().Delete(&token).Error)
				require.NoError(t, db.Unscoped().Delete(&user).Error)
				require.NoError(t, db.Unscoped().Delete(&channel).Error)
			})
			group := tc.groupRatio
			if group == 0 {
				group = 1
			}
			request := &billingexpr.RequestInput{Body: []byte(`{"fast":true}`)}
			cost, trace, err := billingexpr.RunExprWithRequest(tc.expression, billingexpr.TokenParams{P: float64(tc.estimate), Len: float64(tc.estimate)}, *request)
			require.NoError(t, err)
			reservation, err := billingexpr.QuotaRoundStrict(cost / 1_000_000 * common.QuotaPerUnit * group)
			require.NoError(t, err)
			snapshot := &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression), QuotaPerUnit: common.QuotaPerUnit, GroupRatio: group, EstimatedTier: trace.MatchedTier, EstimatedBillingUnit: trace.BillingUnit, EstimatedFixedPrice: trace.FixedPrice, EstimatedQuotaAfterGroup: reservation}
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}, OriginModelName: "fixed-test", UsingGroup: "default", UserGroup: "default", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, ForcePreConsume: true, StartTime: time.Now(), IsStream: tc.stream, RelayFormat: types.RelayFormatOpenAI, PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: group}}, TieredBillingSnapshot: snapshot, BillingRequestInput: request}
			info.SetEstimatePromptTokens(tc.estimate)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			apiErr := service.PreConsumeBilling(ctx, reservation, info)
			if tc.insufficient {
				require.NotNil(t, apiErr)
				assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
			} else {
				require.Nil(t, apiErr)
				held, err := model.GetUserQuota(user.Id, true)
				require.NoError(t, err)
				assert.Equal(t, startingQuota-reservation, held)
				if tc.refund {
					refunded := make(chan struct{}, 1)
					const callback = "fixed_billing_refund_observed"
					require.NoError(t, db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
						if tx.Statement.Table == "tokens" && tx.Error == nil {
							select {
							case refunded <- struct{}{}:
							default:
							}
						}
					}))
					t.Cleanup(func() { require.NoError(t, db.Callback().Update().Remove(callback)) })
					info.Billing.Refund(ctx)
					info.Billing.Refund(ctx)
					select {
					case <-refunded:
					case <-time.After(5 * time.Second):
						t.Fatal("refund did not finish")
					}
				} else {
					if tc.tool {
						info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{"fixed_billing_tool": {CallCount: 1}}}
					}
					if tc.audio {
						billinggateway.PostAudioConsumeQuota(ctx, info, tc.usage, "")
					} else {
						billinggateway.PostTextConsumeQuota(ctx, info, tc.usage, nil)
					}
					require.NoError(t, info.Billing.Settle(tc.want), "a repeated settlement must not charge again")
					var log model.Log
					require.NoError(t, db.Where("user_id = ?", user.Id).Take(&log).Error)
					assert.Equal(t, tc.want, log.Quota)
					assert.Equal(t, tc.stream, log.IsStream)
					assert.NotContains(t, log.Content, "无法扣费")
					var other map[string]any
					require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
					assert.Equal(t, string(tc.unit), other["billing_unit"])
					if tc.unit == billingexpr.BillingUnitRequest {
						assert.Contains(t, other, "fixed_price")
					} else {
						assert.NotContains(t, other, "fixed_price")
					}
				}
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, quota-tc.want, user.Quota)
			assert.Equal(t, startingQuota-tc.want, token.RemainQuota)
			assert.Equal(t, tc.want, user.UsedQuota)
			assert.Equal(t, tc.want, token.UsedQuota)
			if !tc.refund && !tc.insufficient {
				assert.Equal(t, 1, user.RequestCount)
			}
		})
	}
}

func isolatedCostPostgres(t *testing.T, dsn string) gorm.Dialector {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	admin := stdlib.OpenDB(*config.Copy())
	t.Cleanup(func() { assert.NoError(t, admin.Close()) })
	schema := "wave2b2_" + common.GetUUID()
	identifier := pgx.Identifier{schema}.Sanitize()
	_, err = admin.ExecContext(t.Context(), "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+identifier+" CASCADE")
		assert.NoError(t, err)
	})
	config.RuntimeParams["search_path"] = schema
	connection := stdlib.OpenDB(*config)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	var actual string
	require.NoError(t, connection.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&actual))
	require.Equal(t, schema, actual)
	return postgres.New(postgres.Config{Conn: connection})
}

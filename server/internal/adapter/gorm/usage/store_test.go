package usage

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func usagePostgres(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	admin := stdlib.OpenDB(*config.Copy())
	t.Cleanup(func() { assert.NoError(t, admin.Close()) })
	schema := "wave2c_" + common.GetUUID()
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
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{})
	require.NoError(t, err)
	var actual, version string
	require.NoError(t, db.Raw("SELECT current_schema()").Scan(&actual).Error)
	require.Equal(t, schema, actual)
	require.NoError(t, db.Raw("SHOW server_version").Scan(&version).Error)
	t.Logf("PostgreSQL %s, isolated schema %s", version, schema)
	return db
}
func TestUsageAdapterPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("host-only: TEST_POSTGRES_DSN is not configured")
	}
	mainDB, logDB := usagePostgres(t, dsn), usagePostgres(t, dsn)
	oldDB, oldLog := model.DB, model.LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 5*time.Second, 10*time.Millisecond)
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
	require.NoError(t, mainDB.AutoMigrate(&model.User{}, &model.Channel{}, &model.Log{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	user := model.User{Username: "usage-postgres", Quota: 1000}
	channel := model.Channel{Name: "usage-postgres", Key: "fixture"}
	require.NoError(t, mainDB.Create(&user).Error)
	require.NoError(t, mainDB.Create(&channel).Error)
	c, _ := gin.CreateTestContext(nil)
	c.Set("username", user.Username)
	c.Set(common.RequestIdKey, "usage-pg-request")
	enabled := true
	gateReads := 0
	store := Store{ConsumeEnabled: func() bool { gateReads++; return enabled }, ExportEnabled: func() bool { return false }}
	in := domain.Consumption{Record: domain.UsageRecord{UserID: user.Id, ChannelID: channel.Id, ModelName: "usage-model", Quota: 7, PromptTokens: 3, CompletionTokens: 4, Group: "default"}, RenderedContent: "usage fixture", Request: domain.RequestFacts{ContextPresent: true, FirstResponseMilliseconds: -1000}}
	store.AddUserConsumption(user.Id, 7)
	store.AddChannelConsumption(channel.Id, 7)
	require.NoError(t, store.WriteConsumption(c, in))
	enabled = false
	require.NoError(t, store.WriteConsumption(c, in))
	require.Equal(t, 2, gateReads, "one late consume gate read per attempt")
	require.NoError(t, store.WriteFailure(c, domain.Failure{Record: in.Record, Content: "usage failure", Consumption: &in}))
	var rows []model.Log
	require.NoError(t, logDB.Order("id").Find(&rows).Error)
	require.Len(t, rows, 2)
	assert.Equal(t, model.LogTypeConsume, rows[0].Type)
	assert.Equal(t, 7, rows[0].Quota)
	assert.Equal(t, model.LogTypeError, rows[1].Type)
	assert.Zero(t, rows[1].Quota)
	assert.Equal(t, rows[0].Other, rows[1].Other)
	assert.Equal(t, "usage-pg-request", rows[0].RequestId)
	var mainRows int64
	require.NoError(t, mainDB.Model(&model.Log{}).Count(&mainRows).Error)
	assert.Zero(t, mainRows)
	require.NoError(t, mainDB.First(&user, user.Id).Error)
	require.NoError(t, mainDB.First(&channel, channel.Id).Error)
	assert.Equal(t, 7, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.EqualValues(t, 7, channel.UsedQuota)

	// An existing URL with an empty Path still produces a present request_path.
	require.NoError(t, store.WriteFailure(c, domain.Failure{
		Record: in.Record, Content: "empty URL path",
		Request: domain.RequestFacts{URLPresent: true},
	}))
	var emptyPath model.Log
	require.NoError(t, logDB.Order("id DESC").First(&emptyPath).Error)
	var fields map[string]any
	require.NoError(t, common.UnmarshalJsonStr(emptyPath.Other, &fields))
	assert.Contains(t, fields, "request_path")
	assert.Equal(t, "", fields["request_path"])
}

func TestUsageWriterRejectsUnsupportedContexts(t *testing.T) {
	store := Store{}
	var nilGin *gin.Context
	for _, ctx := range []context.Context{nil, context.Background(), nilGin, WithLogMetadata(context.Background(), nil, nil)} {
		require.ErrorContains(t, store.WriteConsumption(ctx, domain.Consumption{}), "requires a non-nil gateway Gin context")
		require.ErrorContains(t, store.WriteFailure(ctx, domain.Failure{}), "requires a non-nil gateway Gin context")
	}
}

func TestSerializerOwnsBillingShapesAndPreservesProviderJSON(t *testing.T) {
	other := consumptionOther(domain.Consumption{
		Request: domain.RequestFacts{
			ContextPresent: true,
			OpenRouterCost: &domain.OpenRouterCostState{
				Basis: billingdomain.ProviderBasis{Version: "v1", EffectiveGroupRatio: 1.5, QuotaPerUnit: 500000, EstimatedUSD: 0.1},
				Final: true,
			},
		},
		Text: &domain.TextFacts{
			Tools:   []billingdomain.ToolSurchargeItem{{Name: "search", Count: 2, Price: 10, DisplayQuota: 999}},
			Outcome: &billingdomain.Outcome{SettlementStatus: "settled", TargetQuota: 17, FundingAppliedQuota: 17},
		},
	}, logMetadata{
		CostDetails: []byte(`{"z":1.00,"a":0}`),
		Affinity:    map[string]any{"rule_name": "fixture", "override_template": map[string]any{"applied": true}},
	})
	encoded, err := common.Marshal(other.Snapshot())
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"openrouter_cost_details":{"z":1.00,"a":0}`)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(encoded, &fields))
	assert.Equal(t, []any{map[string]any{"name": "search", "count": float64(2), "price": float64(10)}}, fields["tool_surcharges"])
	admin, ok := fields["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{
		"target_quota": float64(17), "reserved_quota": float64(0),
		"funding_applied_quota": float64(17), "token_applied_quota": float64(0),
		"settlement_status": "settled",
	}, admin["openrouter_settlement"])
	assert.Equal(t, map[string]any{
		"version": "v1", "effective_group_ratio": 1.5, "quota_per_unit": float64(500000), "estimated_usd": 0.1,
	}, admin["openrouter_cost_basis"])
	assert.Contains(t, admin, "channel_affinity")
}
func TestRendererOwnsTextAndAudioNoteJoining(t *testing.T) {
	renderer := Content{Quota: func(int) string { return "17" }}
	assert.Equal(t, ", search 调用 2 次，调用花费 17", renderer.RenderContent([]domain.Note{
		{Kind: domain.LiteralContent},
		{Kind: domain.ToolCharge, Name: "search", Count: 2, Quota: 17},
	}))
	assert.Equal(t, "audio（可能是上游超时）, extra", renderer.RenderContent([]domain.Note{
		{Kind: domain.LiteralContent, Name: "audio"},
		{Kind: domain.PossibleTimeout, Join: domain.Adjacent},
		{Kind: domain.LiteralContent, Name: "extra"},
	}))
}

func TestAppendToolSurchargeLogInfoWritesOnlyStructuredFields(t *testing.T) {
	items := []billingdomain.ToolSurchargeItem{
		{Name: "web_search", Count: 2, Price: 10},
		{Name: "image_generation", Count: 1, Price: 150},
	}
	other := consumptionOther(domain.Consumption{Text: &domain.TextFacts{Tools: items}}, logMetadata{})

	fields := other.Snapshot()
	encodedTools, err := common.Marshal(fields["tool_surcharges"])
	require.NoError(t, err)
	var storedTools []map[string]any
	require.NoError(t, common.Unmarshal(encodedTools, &storedTools))
	assert.Equal(t, []map[string]any{
		{"name": "web_search", "count": float64(2), "price": float64(10)},
		{"name": "image_generation", "count": float64(1), "price": float64(150)},
	}, storedTools)
	assert.NotContains(t, fields, "web_search")
	assert.NotContains(t, fields, "web_search_call_count")
	assert.NotContains(t, fields, "web_search_price")
	assert.NotContains(t, fields, "file_search")
	assert.NotContains(t, fields, "image_generation_call")
	assert.NotContains(t, fields, "image_generation_call_price")
}

func TestSerializerPreservesTieredRulesAndConversionDiagnostics(t *testing.T) {
	other := consumptionOther(domain.Consumption{
		Kind: domain.ChannelTestConsumption,
		Request: domain.RequestFacts{
			ContextPresent: true,
			Diagnostics:    []domain.Diagnostic{{Code: "unsupported_hosted_tool", Message: "fixture diagnostic"}},
			Tiered: &domain.Tiered{
				ExprString: `tier("base", p * 2)`,
				Actual: &domain.TieredResult{MatchedTier: "base", RequestRules: []billingdomain.RequestRuleTrace{{
					Cond: `param("service_tier") == "fast"`, Multiplier: 2, Matched: true,
				}}},
			},
		},
		Prices: domain.Prices{GroupRatio: 1, CacheTokens: 12},
	}, logMetadata{})
	encoded, err := common.Marshal(other.Snapshot())
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, common.Unmarshal(encoded, &fields))
	assert.Equal(t, "tiered_expr", fields["billing_mode"])
	assert.Equal(t, "base", fields["matched_tier"])
	assert.Equal(t, []any{map[string]any{
		"cond": `param("service_tier") == "fast"`, "multiplier": float64(2), "matched": true,
	}}, fields["request_rules"])
	assert.NotEmpty(t, fields["expr_b64"])
	admin, ok := fields["admin_info"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{map[string]any{
		"code": "unsupported_hosted_tool", "message": "fixture diagnostic", "from": "", "to": "", "severity": "",
	}}, admin["conversion_diagnostics"])
}

func TestStoredFXUsesBillingValidationAndCapturedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fx      domain.FX
		present bool
	}{
		{name: "captured", fx: domain.FX{Rate: 200, Factor: 2, Source: "fixture", EffectiveAt: 1, FetchedAt: 1}, present: true},
		{name: "NaN rate", fx: domain.FX{Rate: math.NaN(), Factor: 2, Source: "fixture", EffectiveAt: 1, FetchedAt: 1}},
		{name: "infinite factor", fx: domain.FX{Rate: 200, Factor: math.Inf(1), Source: "fixture", EffectiveAt: 1, FetchedAt: 1}},
		{name: "mismatched factor", fx: domain.FX{Rate: 200, Factor: 3, Source: "fixture", EffectiveAt: 1, FetchedAt: 1}},
		{name: "missing source", fx: domain.FX{Rate: 200, Factor: 2, EffectiveAt: 1, FetchedAt: 1}},
		{name: "missing capture time", fx: domain.FX{Rate: 200, Factor: 2, Source: "fixture", EffectiveAt: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := model.NewLogOther()
			appendBillingFX(other, &domain.RequestFacts{FX: tc.fx}, domain.PriceFacts{}, "request")
			admin, _ := other.Snapshot()["admin_info"].(map[string]any)
			assert.Equal(t, tc.present, admin["billing_fx"] != nil)
		})
	}
}

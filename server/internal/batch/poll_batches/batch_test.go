package poll_batches

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	diagnostics "github.com/QuantumNous/new-api/internal/adapter/batchruntime"
	math "github.com/QuantumNous/new-api/internal/adapter/billing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/batch"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	rediscache "github.com/QuantumNous/new-api/internal/adapter/redis/batch"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/apikeystorage"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/batch"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBatchPricingClockIsFrozen(t *testing.T) {
	at := time.Date(2001, 2, 3, 4, 5, 0, 0, time.UTC)
	value, _, err := billingexpr.RunExprWithRequest(`tier("frozen", float(hour("UTC") + minute("UTC") + day("UTC") + month("UTC")))`, billingexpr.TokenParams{}, billingexpr.RequestInput{EvaluatedAt: at})
	require.NoError(t, err)
	assert.Equal(t, 14.0, value)
}

func TestBatchCanonicalPurchaseUsage(t *testing.T) {
	require.NoError(t, i18n.Init())
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
			nativeUsage, err := (gateway.Usage{}).Facts((*domain.Job)(job), domain.Result(result))
			usage := nativeUsage.Usage
			require.NoError(t, err)
			snapshot := math.BatchBillingSnapshot{PurchaseExpression: tc.expression, Client: billingexpr.BillingSnapshot{ExprString: tc.expression, ExprHash: billingexpr.ExprHashString(tc.expression), GroupRatio: 2, QuotaPerUnit: 1000000, ExprVersion: 1}}
			purchase := testPolling().purchaseUSD((*domain.Job)(job), domain.Result(result), usage, pureSnapshot(t, snapshot), []byte(`{}`))
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
			require.NoError(t, reserve(job, []model.BatchItem{
				{CustomID: "missing", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
				{CustomID: "ready", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
				{CustomID: "next", Body: `{}`, UpstreamBody: `{}`, BillingStatus: "reserved", ReservedQuota: 10},
			}))
			missing, err := common.Marshal(batch.Result{StatusCode: 200, Body: []byte(`{}`)})
			require.NoError(t, err)
			ready, err := common.Marshal(result)
			require.NoError(t, err)
			require.NoError(t, testStore().StoreBatchResult(job.ID, "missing", string(missing), "missing"))
			require.NoError(t, testStore().StoreBatchResult(job.ID, "ready", string(ready), "ready"))
			require.NoError(t, testStore().StoreBatchResult(job.ID, "next", string(ready), "next"))
			require.NoError(t, testPolling().SettleResults((*domain.Job)(job)))
			require.NoError(t, testPolling().SettleResults((*domain.Job)(job)))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 100-int(tc.cost*4), user.Quota, "both ready items settle despite debt and earlier missing usage")
			var item model.BatchItem
			require.NoError(t, db.Where("batch_id = ? AND custom_id = ?", job.ID, "ready").First(&item).Error)
			assert.EqualValues(t, int(tc.cost*2), item.ChargedQuota)
			var details domain.FinancialDetails
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
			taskfixture.Truncate(t)
			t.Cleanup(func() {
				for _, table := range []string{"batch_log_events", "batch_quota_events", "batch_items", "batch_jobs"} {
					require.NoError(t, model.DB.Exec("DELETE FROM "+table).Error)
				}
			})
			taskfixture.SeedUser(t, 835, 10000)
			taskfixture.SeedToken(t, 835, 835, "or-batch-cost", 10000)
			taskfixture.SeedChannel(t, 835)
			basis := common.OpenRouterCostBasis{Version: domain.BatchSettlementOpenRouterCostV1, EffectiveGroupRatio: 4, QuotaPerUnit: 500000, EstimatedUSD: 0.000025}
			encoded, err := common.Marshal(math.BatchBillingSnapshot{SettlementMode: domain.BatchSettlementOpenRouterCostV1, OpenRouterCost: &basis})
			require.NoError(t, err)
			job := &model.BatchJob{PublicID: "or-cost-test", UserID: 835, TokenID: 835, ChannelID: 835, ChannelType: constant.ChannelTypeOpenRouter, Model: "or/model", Endpoint: "/v1/chat/completions", BillingSnapshot: string(encoded), RequestCount: 1, ReservedQuota: 50, UpstreamUsage: `{"cost":99}`}
			require.NoError(t, reserve(job, []model.BatchItem{{CustomID: "item", Body: `{}`, UpstreamBody: `{}`, ReservedQuota: 50, BillingStatus: "reserved"}}))
			result, err := common.Marshal(batch.Result{CustomID: "item", StatusCode: 200, Body: []byte(tc.body), Unbilled: tc.unbilled})
			require.NoError(t, err)
			require.NoError(t, testStore().StoreBatchResult(job.ID, "item", string(result), "result"))
			require.NoError(t, testPolling().SettleResults((*domain.Job)(job)))
			require.NoError(t, testPolling().SettleResults((*domain.Job)(job)))
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
					assert.Contains(t, item.FinancialDetails, domain.BatchSettlementOpenRouterCostV1)
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
func TestBatchPurchaseUsesFrozenProviderBase(t *testing.T) {
	snapshot := math.BatchBillingSnapshot{PurchaseExpression: `tier("batch", p * 1 + c * 2)`, Client: billingexpr.BillingSnapshot{ExprString: `tier("batch", p * 1 + c * 2)`, GroupRatio: 1.8, QuotaPerUnit: 500000}}
	result := batch.Result{StatusCode: 200, Body: []byte(`{"usage":{"prompt_tokens":200,"completion_tokens":50}}`)}
	job := &model.BatchJob{ChannelType: constant.ChannelTypeOpenAI, Endpoint: "/v1/chat/completions"}
	nativeUsage, err := (gateway.Usage{}).Facts((*domain.Job)(job), domain.Result(result))
	usage := nativeUsage.Usage
	require.NoError(t, err)
	purchase := testPolling().purchaseUSD((*domain.Job)(job), domain.Result(result), usage, pureSnapshot(t, snapshot), []byte(`{}`))
	require.NotNil(t, purchase)
	assert.InDelta(t, 0.0003, *purchase, 1e-12, "procurement excludes group and FX")
	params := billingexpr.TokenParams(billingdomain.BuildTieredTokenParams(usage, false, billingexpr.UsedVars(snapshot.Client.ExprString)))
	charge, err := billingexpr.ComputeTieredQuotaWithRequest(&snapshot.Client, params, billingexpr.RequestInput{EvaluatedAt: snapshot.EvaluatedAt})
	require.NoError(t, err)
	assert.Equal(t, 270, charge.ActualQuotaAfterGroup, "same base times group=2 and FX=0.9")
	snapshot.PurchaseExpression = `tier("legacy", p * 0.5 + c * 1)`
	purchase = testPolling().purchaseUSD((*domain.Job)(job), domain.Result(result), usage, pureSnapshot(t, snapshot), []byte(`{}`))
	require.NotNil(t, purchase)
	assert.InDelta(t, 0.00015, *purchase, 1e-12, "existing batches retain their frozen procurement basis")
	snapshot.PurchaseExpression = ""
	assert.Nil(t, testPolling().purchaseUSD((*domain.Job)(job), domain.Result(result), usage, pureSnapshot(t, snapshot), []byte(`{}`)), "missing legacy procurement must not be reinterpreted")
}

func testPolling() Usecase {
	return NewUsecase(testStore(), gateway.Gateway{}, math.BatchCodec{}, math.BatchMath{}, usageadapter.BatchMetadata{}, diagnostics.Diagnostics{}, time.Now, domain.ChannelKinds{OpenAI: constant.ChannelTypeOpenAI, Anthropic: constant.ChannelTypeAnthropic, Google: constant.ChannelTypeGemini, OpenRouter: constant.ChannelTypeOpenRouter})
}
func pureSnapshot(t *testing.T, s math.BatchBillingSnapshot) domain.BillingSnapshot {
	t.Helper()
	raw, err := common.Marshal(s)
	require.NoError(t, err)
	out, err := (math.BatchCodec{}).Snapshot(string(raw))
	require.NoError(t, err)
	return out
}

func TestMain(m *testing.M) { taskfixture.Main(m) }

// These seams exist only in the owning test binary, never in production.
func (u Usecase) RecordResult(job *domain.Job, result domain.Result) error {
	return u.recordResult(job, result)
}
func (u Usecase) SettleResults(job *domain.Job) error     { return u.settleResults(job) }
func (u Usecase) ReconcilePurchase(job *domain.Job) error { return u.reconcilePurchase(job) }

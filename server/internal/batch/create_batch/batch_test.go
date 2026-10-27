package create_batch

import (
	fxstorage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	fxstate "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	pricingdomainfx "github.com/QuantumNous/new-api/internal/pricing/domain"

	"errors"
	"github.com/QuantumNous/new-api/gateway/tokenusage"
	math "github.com/QuantumNous/new-api/internal/adapter/billing"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	prices "github.com/QuantumNous/new-api/internal/adapter/settings/batch"
	billingapp "github.com/QuantumNous/new-api/internal/app/billing"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/contracttest/taskfixture"

	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	seedFX(t) // USD=90, so the frozen FX factor is 0.9.
	settings.BatchBillingExpr = map[string]string{"provider-base": `tier("batch", p * 1 + c * 2)`}
	snapshot, err := testCreation().capture("provider-base", domain.AdmissionFacts{UserGroup: "default", UsingGroup: "default", ChannelType: constant.ChannelTypeOpenAI})
	require.NoError(t, err)
	orSnapshot, err := testCreation().capture("no-display-expression", domain.AdmissionFacts{UserGroup: "default", UsingGroup: "default", ChannelType: constant.ChannelTypeOpenRouter})
	require.NoError(t, err)
	require.NotNil(t, orSnapshot.OpenRouterCost)
	assert.Equal(t, domain.BatchSettlementOpenRouterJobCostV2, orSnapshot.SettlementMode)
	assert.InDelta(t, 1.8, orSnapshot.OpenRouterCost.EffectiveGroupRatio, 1e-12)
	assert.Empty(t, orSnapshot.Client.ExprString, "display availability does not gate OR admission")
	settings.BatchBillingExpr["provider-base"] = `tier("changed", p * 100)`

	assert.Equal(t, `tier("batch", p * 1 + c * 2)`, snapshot.PurchaseExpression)
	assert.InDelta(t, 1.8, snapshot.Client.GroupRatio, 1e-12)
}
func TestBatchTokenEstimates(t *testing.T) {
	oldCount := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() { constant.CountToken = oldCount })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/batches", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "different-parent-model")
	snapshot := math.BatchBillingSnapshot{Client: billingexpr.BillingSnapshot{ModelName: "gpt-4o", ExprString: `tier("tokens", p + c)`, ExprHash: billingexpr.ExprHashString(`tier("tokens", p + c)`), GroupRatio: 1, QuotaPerUnit: 1000000, ExprVersion: 1}}
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
			want := tokenusage.CountTextToken(tc.request.GetTokenCountMeta().CombineText, "gpt-4o") + tc.framing + tc.output
			for _, raw := range []string{tc.body, " \n" + strings.TrimSuffix(tc.body, "}") + `,"opaque_provider_extension":{"ignored":"` + strings.Repeat("x", 1000) + `"}}`} {
				body := []byte(raw)
				quota, err := testCreation().estimate(tokenAdmission{c: c, endpoint: tc.endpoint}, pureSnapshot(t, snapshot), body, tc.output)
				require.NoError(t, err)
				assert.Equal(t, want, quota)
				assert.Equal(t, raw, string(body), "counting preserves raw provider fields")
			}
		})
	}
	body, err := relaycommon.ApplyParamOverride([]byte(`{"model":"mapped","messages":[{"role":"user","content":"original"}],"max_tokens":10,"temperature":0,"opaque":false}`), map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello world"}}}, nil)
	require.NoError(t, err)
	quota, err := testCreation().estimate(tokenAdmission{c: c, endpoint: "/v1/chat/completions"}, pureSnapshot(t, snapshot), body, 10)
	require.NoError(t, err)
	var prepared dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(body, &prepared))
	assert.Equal(t, tokenusage.CountTextToken(prepared.GetTokenCountMeta().CombineText, "gpt-4o")+16, quota, "estimate uses prepared overrides")
	assert.Contains(t, string(body), `"temperature":0`)
	assert.Contains(t, string(body), `"opaque":false`)
	assert.Contains(t, string(body), `"model":"mapped"`)
	assert.Equal(t, "different-parent-model", common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
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

type tokenAdmission struct {
	domain.Admission
	c        *gin.Context
	endpoint string
}

func (s tokenAdmission) Count(model string, body []byte) (int, error) {
	return gateway.CountBatchItemTokens(s.c, model, s.endpoint, body)
}
func testCreation() Usecase {
	fx := billingapp.NewCosts().FX
	return Usecase{engine: math.BatchMath{}, prices: prices.Prices{}, fx: func() (billingdomain.FXBasis, error) { return fx.Execute(billingdomain.FXBasis{}) }, now: time.Now, openRouter: constant.ChannelTypeOpenRouter, quotaUnit: func() float64 { return common.QuotaPerUnit }}
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

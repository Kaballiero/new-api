package billing_setting_test

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGPT6AstraBuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	savedOptions := common.OptionMap
	t.Cleanup(func() {
		*settings, common.OptionMap = saved, savedOptions
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	common.OptionMap = map[string]string{"billing_setting.billing_mode": `{}`, "billing_setting.billing_expr": `{}`}
	require.NoError(t, config.GlobalConfig.LoadFromDB(common.OptionMap))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode("gpt-6-astra"))
	expression, ok := billing_setting.GetBillingExpr("gpt-6-astra")
	require.True(t, ok)

	for _, tc := range []struct {
		name                           string
		input, output, cached, written int
		request                        string
		quota                          int
	}{
		{"standard", 1000, 100, 0, 0, `{}`, 7500},
		{"client flex cannot discount standard pricing", 1000, 100, 0, 0, `{"service_tier":"flex"}`, 7500},
		{"cache at context boundary", 272000, 1000, 200000, 20000, `{}`, 510000},
		{"whole request above boundary", 272001, 1000, 200000, 20000, `{}`, 1007510},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := &dto.Usage{
				PromptTokens: tc.input, CompletionTokens: tc.output,
				PromptTokensDetails: dto.InputTokenDetails{CachedTokens: tc.cached, CacheWriteTokens: tc.written},
			}
			params := service.BuildTieredTokenParams(usage, false, billingexpr.UsedVars(expression))
			result, err := billingexpr.ComputeTieredQuotaWithRequest(&billingexpr.BillingSnapshot{
				ExprString: expression, GroupRatio: 1, QuotaPerUnit: 500000,
			}, params, billingexpr.RequestInput{Body: []byte(tc.request)})
			require.NoError(t, err)
			assert.Equal(t, tc.quota, result.ActualQuotaAfterGroup)
		})
	}

	t.Run("admin options expose defaults without persisting them", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		controller.GetOptions(ctx)
		var response struct {
			Success bool
			Data    []model.Option
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		found := map[string]string{}
		for _, option := range response.Data {
			if _, ok := common.OptionMap[option.Key]; ok {
				assert.NotContains(t, found, option.Key)
				var values map[string]string
				require.NoError(t, common.UnmarshalJsonStr(option.Value, &values))
				found[option.Key] = values["gpt-6-astra"]
				assert.Equal(t, `{}`, common.OptionMap[option.Key])
			}
		}
		assert.Equal(t, map[string]string{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}, found)
	})

	for _, tc := range []struct {
		name, mode, expr, ratios, prices, wantMode string
	}{
		{"custom expression overrides legacy price", "tiered_expr", "p * 7", `{"gpt-6-astra":8}`, `{}`, "tiered_expr"},
		{"explicit ratio mode", "ratio", "", `{}`, `{}`, "ratio"},
		{"existing free token price", "", "", `{"gpt-6-astra":0}`, `{}`, "ratio"},
		{"existing per-call price", "", "", `{}`, `{"gpt-6-astra":0.1}`, "ratio"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
			if tc.mode != "" {
				settings.BillingMode["gpt-6-astra"] = tc.mode
			}
			if tc.expr != "" {
				settings.BillingExpr["gpt-6-astra"] = tc.expr
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(tc.ratios))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(tc.prices))
			assert.Equal(t, tc.wantMode, billing_setting.GetBillingMode("gpt-6-astra"))
			actual, ok := billing_setting.GetBillingExpr("gpt-6-astra")
			assert.Equal(t, tc.expr, actual)
			assert.Equal(t, tc.expr != "", ok)
		})
	}
}

func TestOpenRouterImageBuiltinBilling(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	saved := *settings
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		*settings = saved
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	for _, tc := range []struct {
		model  string
		priced bool
	}{
		{"tencent/hy-image-v3.5-preview", true},
		{"bytedance-seed/seedream-5-0-flash", true},
		{"black-forest-labs/flux-3-image", true},
		{"inclusionai/ming-image-0.1-design-layer", true},
		{"recraft/recraft-v4.1-flash", true},
		{"inclusionai/ming-image-0.1-design", true},
		{"openai/gpt-image-2.5-sunburst", true},
		{"openai/gpt-image-2.5-flare", true},
		{"microsoft/mai-image-2.6", true},
		{"microsoft/mai-image-2.6-flash", true},
		{"meta/muse-image", true},
		{"recraft/recraft-v4-styles-pro", true},
		{"recraft/recraft-v4-styles-vector", true},
		{"recraft/recraft-v4-styles-pro-vector", true},
		{"recraft/recraft-v4-styles", true},
		{"bytedance-seed/seedream-5-0-lite", true},
		{"bytedance-seed/seedream-5-0-pro", true},
		{"x-ai/grok-imagine-image-2.0", true},
		{"qwen/qwen-image-3-pro", true},
		{"qwen/qwen-image-3", true},
		{"microsoft/mai-image-2.5-pro", true},
		{"krea/krea-2-large", true},
		{"krea/krea-2-medium", true},
		{"krea/krea-2-medium-turbo", true},
		{"openai/gpt-image-2", true},
		{"openai/gpt-image-1-mini", true},
		{"openai/gpt-image-1", true},
		{"sourceful/riverflow-v2.5-pro", true},
		{"sourceful/riverflow-v2.5-fast", true},
		{"microsoft/mai-image-2.5", true},
		{"x-ai/grok-imagine-image-quality", true},
		{"recraft/recraft-v4.1-pro-vector", true},
		{"recraft/recraft-v4.1-vector", true},
		{"recraft/recraft-v4.1-utility-pro", true},
		{"recraft/recraft-v4.1-utility", true},
		{"recraft/recraft-v4.1-pro", true},
		{"recraft/recraft-v4.1", true},
		{"recraft/recraft-v4-pro-vector", true},
		{"recraft/recraft-v4-vector", true},
		{"recraft/recraft-v4-pro", true},
		{"recraft/recraft-v4", true},
		{"recraft/recraft-v3", true},
		{"sourceful/riverflow-v2-pro", true},
		{"sourceful/riverflow-v2-fast", true},
		{"black-forest-labs/flux.2-klein-4b", true},
		{"bytedance-seed/seedream-4.5", true},
		{"black-forest-labs/flux.2-max", true},
		{"black-forest-labs/flux.2-flex", true},
		{"black-forest-labs/flux.2-pro", true},
	} {
		expression, exists := billing_setting.GetBuiltinBillingExpr(tc.model)
		assert.Equal(t, tc.priced, exists, tc.model)
		if !tc.priced {
			continue
		}
		assert.Equal(t, `tier("openrouter", provider_cost * 1000000)`, expression)
		assert.Equal(t, billing_setting.BillingModeTieredExpr, billing_setting.GetBillingMode(tc.model))
		for _, cost := range []float64{0, 0.01, 0.607} {
			params := service.BuildTieredTokenParams(&dto.Usage{Cost: cost}, false, billingexpr.UsedVars(expression))
			result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{ExprString: expression, ExprHash: billingexpr.ExprHashString(expression), GroupRatio: 1.5, QuotaPerUnit: 500000}, params)
			require.NoError(t, err)
			assert.Equal(t, common.QuotaRound(cost*500000*1.5), result.ActualQuotaAfterGroup)
		}
	}
	assert.NotContains(t, billing_setting.GetBuiltinBillingExprCopy(), "google/gemini-3-pro-image-preview")
	const modelName = "black-forest-labs/flux-3-image"
	settings.BillingExpr[modelName] = `tier("admin", fixed(0.2))`
	expression, exists := billing_setting.GetBillingExpr(modelName)
	require.True(t, exists)
	assert.Equal(t, settings.BillingExpr[modelName], expression)
	delete(settings.BillingExpr, modelName)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"black-forest-labs/flux-3-image":0}`))
	assert.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode(modelName))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"black-forest-labs/flux-3-image":0.1}`))
	assert.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode(modelName))
}

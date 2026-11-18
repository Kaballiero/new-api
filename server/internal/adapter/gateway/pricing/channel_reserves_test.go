package pricing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/app/pricingruntime"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/preview_channel_reserves"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Preview prices the reference request with the admission rules of the
// gateway and names the channel reserve policy as PrepareRequest does; it never
// discovers live endpoints.
func TestChannelReservesPreview(t *testing.T) {
	previous := pricingruntime.UpstreamCatalogTariffs.Load()
	t.Cleanup(func() { pricingruntime.UpstreamCatalogTariffs.Store(previous) })
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices)) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"public-model":5}`))
	tariff := func(tag, pricing string, output ...string) domain.NativeEndpointResponse {
		var prices map[string]json.RawMessage
		require.NoError(t, common.UnmarshalJsonStr(pricing, &prices))
		return domain.NativeEndpointResponse{Endpoints: []domain.NativePricedEndpoint{{Tag: tag, MaxCompletionTokens: 100, Pricing: prices}}, OutputModalities: output}
	}
	// unitTariff prices one unit of a speech or rerank endpoint at usd.
	unitTariff := func(billable domain.PriceBillable, unit domain.PriceUnit, usd string, output string) domain.NativeEndpointResponse {
		response := tariff("provider", `{"prompt":"0","completion":"0"}`, output)
		amount := decimal.RequireFromString(usd)
		response.Endpoints[0].UnitPrices = []domain.EndpointUnitPrice{{Dimension: domain.PriceDimension{Billable: billable, Unit: unit}, USD: &amount}}
		return response
	}
	pricingruntime.UpstreamCatalogTariffs.Store(&domain.UpstreamCatalogTariffs{FetchedAt: time.Now(), Models: map[string]domain.NativeEndpointResponse{
		"test/speech":                    unitTariff(domain.PriceBillableOutputSpeech, domain.PriceUnitCharacter, "0.00003", "speech"),
		"test/rerank":                    unitTariff(domain.PriceBillableSearch, domain.PriceUnitSearch, "0.002", "rerank"),
		"test/model":                     tariff("provider", `{"prompt":"0.001","completion":"0.002"}`, "text"),
		"test/search":                    tariff("provider", `{"prompt":"0.001","completion":"0.002","web_search":"0.01"}`, "text"),
		"test/embed":                     tariff("provider", `{"prompt":"0.001"}`, "embeddings"),
		"perplexity/sonar-deep-research": tariff("perplexity", `{"prompt":"0.001","completion":"0.002","web_search":"0.01","internal_reasoning":"0"}`, "text"),
	}})
	// Every live discovery would reach this server; an unbound estimator never
	// sends one.
	var liveCalls atomic.Int32
	live := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { liveCalls.Add(1) }))
	t.Cleanup(live.Close)
	configuredUSD := 5.0

	for _, tc := range []struct {
		name, model, operation, setting, baseURL string
		want                                     preview_channel_reserves.ReservePreview
		wantErr                                  error
	}{
		{name: "floor below the estimate of a web_search endpoint", model: "test/search", setting: `{"openrouter_model_reserve_usd":{"test/search":0.05}}`,
			want: preview_channel_reserves.ReservePreview{USD: 1.21, Source: domain.ReserveSourceCatalog, AdmissionPolicy: domain.AdmissionPolicyModelReserveFloor}},
		{name: "floor above the estimate is the manual reserve", model: "test/model", setting: `{"openrouter_model_reserve_usd":{"test/model":3}}`,
			want: preview_channel_reserves.ReservePreview{USD: 3, Source: domain.ReserveSourceManual, AdmissionPolicy: domain.AdmissionPolicyModelReserveFloor}},
		{name: "whole hold of a configured price with a channel reserve", model: "test/model", setting: `{"openrouter_reservation_source":{"test/model":"configured_price"},"openrouter_model_reserve_usd":{"test/model":0.05}}`,
			want: preview_channel_reserves.ReservePreview{USD: 5, Source: domain.ReserveSourceConfiguredPrice, AdmissionPolicy: domain.AdmissionPolicyModelApproximateReserve, ConfiguredUSD: &configuredUSD}},
		{name: "configured price passes through", model: "test/model", setting: `{"openrouter_reservation_source":{"test/model":"configured_price"}}`,
			want: preview_channel_reserves.ReservePreview{USD: 5, Source: domain.ReserveSourceConfiguredPrice, ConfiguredUSD: &configuredUSD}},
		{name: "legacy Sonar research reserve", model: "perplexity/sonar-deep-research", setting: `{"openrouter_sonar_research_reserve_usd":2}`,
			want: preview_channel_reserves.ReservePreview{USD: 2, Source: domain.ReserveSourceManual, AdmissionPolicy: domain.AdmissionPolicySonarResearchApproximateReserve}},
		{name: "embeddings", model: "test/embed", operation: "embeddings",
			want: preview_channel_reserves.ReservePreview{USD: 1, Source: domain.ReserveSourceCatalog}},
		{name: "speech of the reference input", model: "test/speech", operation: "audio/speech",
			want: preview_channel_reserves.ReservePreview{USD: .003, Source: domain.ReserveSourceCatalog}},
		{name: "speech reserve above the estimate is the manual reserve", model: "test/speech", operation: "audio/speech", setting: `{"openrouter_model_reserve_usd":{"test/speech":0.1}}`,
			want: preview_channel_reserves.ReservePreview{USD: .1, Source: domain.ReserveSourceManual, AdmissionPolicy: domain.AdmissionPolicyModelApproximateReserve}},
		{name: "rerank of the reference documents", model: "test/rerank", operation: "rerank",
			want: preview_channel_reserves.ReservePreview{USD: .002, Source: domain.ReserveSourceCatalog}},
		{name: "rerank reserve below the estimate keeps the catalog source", model: "test/rerank", operation: "rerank", setting: `{"openrouter_model_reserve_usd":{"test/rerank":0.001}}`,
			want: preview_channel_reserves.ReservePreview{USD: .002, Source: domain.ReserveSourceCatalog, AdmissionPolicy: domain.AdmissionPolicyModelApproximateReserve}},
		{name: "outside the catalog", model: "other/model", baseURL: live.URL, wantErr: domain.ErrEndpointDiscoveryMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := "https://openrouter.ai/api"
			if tc.baseURL != "" {
				base = tc.baseURL
			}
			operation := "chat/completions"
			if tc.operation != "" {
				operation = tc.operation
			}
			channel := &domain.BuildChannel{Id: 1, Type: constant.ChannelTypeOpenRouter, BaseURL: &base, Setting: &tc.setting}

			preview, err := ChannelReserves{}.Preview(channel, preview_channel_reserves.ReferenceRequest{Model: "public-model", UpstreamModel: tc.model, Operation: operation, PromptTokens: 1000})

			assert.Zero(t, liveCalls.Load())
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want.USD, preview.USD, 1e-9)
			preview.USD = tc.want.USD
			assert.Equal(t, tc.want, preview)
		})
	}
}

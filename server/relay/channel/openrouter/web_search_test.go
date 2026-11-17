package openrouter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A request reserves its web searches at the web_search price of the
// endpoint: the openrouter:web_search tools their max_uses or else 30, together
// at most max_tool_calls and 30, plus one for the web plugin and one for
// web_search_options. A search engine other than native has no endpoint price
// and adds the channel reserve.
func TestEndpointWebSearchReserve(t *testing.T) {
	const tokens = `,"max_tokens":10`
	const tool = `,"tools":[{"type":"openrouter:web_search"`
	for _, tc := range []struct {
		name, extra, operation, wantError string
		// otherModel routes the request to a second model priced without
		// web_search.
		otherModel   bool
		searchAbsent bool
		reserve      *float64
		want         float64
		wantSearches int
		wantUse      ReserveUse
	}{
		{name: "web plugin is one search", extra: `,"plugins":[{"id":"web"}]`, want: 1.03, wantSearches: 1},
		{name: "web plugin on the native engine", extra: `,"plugins":[{"id":"web","engine":"native","max_results":5}]`, want: 1.03, wantSearches: 1},
		{name: "web_search_options is one search", extra: `,"web_search_options":{"search_context_size":"high"}`, want: 1.03, wantSearches: 1},
		{name: "server tool without max_uses is 30 searches", extra: tool + `}]`, want: 1.32, wantSearches: 30},
		{name: "server tool max_uses", extra: tool + `,"parameters":{"max_uses":3}}]`, want: 1.05, wantSearches: 3},
		{name: "server tool on the auto engine", extra: tool + `,"parameters":{"engine":"auto","max_uses":3}}]`, want: 1.05, wantSearches: 3},
		{name: "server tool max_uses above 30 is capped", extra: tool + `,"parameters":{"max_uses":1000000}}]`, want: 1.32, wantSearches: 30},
		{name: "max_tool_calls caps the searches", extra: tool + `,"parameters":{"max_uses":3}}],"max_tool_calls":2`, want: 1.04, wantSearches: 2},
		{name: "all sources add up", extra: tool + `,"parameters":{"max_uses":2}},{"type":"function","function":{"name":"f"}}],"plugins":[{"id":"web"}],"web_search_options":{}`, want: 1.06, wantSearches: 4},
		{name: "max_tool_calls caps only the server tool", extra: tool + `,"parameters":{"max_uses":3}}],"plugins":[{"id":"web"}],"max_tool_calls":1`, want: 1.04, wantSearches: 2},
		{name: "plugin and web_search_options add to the server tool cap", extra: tool + `}],"plugins":[{"id":"web"}],"web_search_options":{}`, want: 1.34, wantSearches: 32},
		{name: "two web plugins are one search", extra: `,"plugins":[{"id":"web"},{"id":"web"}]`, want: 1.03, wantSearches: 1},
		{name: "plugin max_tool_calls stays unchecked", extra: `,"plugins":[{"id":"web"}],"max_tool_calls":0`, want: 1.03, wantSearches: 1},
		{name: "two server tools are capped at 30", extra: tool + `},{"type":"openrouter:web_search"}]`, want: 1.32, wantSearches: 30},
		{name: "Responses server tool", extra: `,"max_output_tokens":10` + tool + `,"parameters":{"max_uses":3}}]`, operation: "responses", want: 1.05, wantSearches: 3},
		{name: "max_tool_calls without search stays unchecked", extra: `,"max_tool_calls":0`, want: 1.02},
		{name: "other model without a search price cannot take the reserve of the model", extra: `,"plugins":[{"id":"web"}]`, otherModel: true, reserve: common.GetPointer(0.5), wantError: "endpoint model does not match final model ID"},
		{name: "missing search price takes the channel reserve", extra: `,"plugins":[{"id":"web"}]`, searchAbsent: true, reserve: common.GetPointer(0.5), want: 1.52, wantSearches: 1, wantUse: ReserveAddOn},
		{name: "missing search price without a reserve stays closed", extra: `,"plugins":[{"id":"web"}]`, searchAbsent: true, wantError: pricingdomain.ErrHostedSearchUnpriced.Error()},
		{name: "exa engine takes the channel reserve", extra: `,"plugins":[{"id":"web","engine":"exa"}]`, reserve: common.GetPointer(0.5), want: 1.52, wantUse: ReserveAddOn},
		{name: "native searches beside the parallel engine add the channel reserve", extra: tool + `,"parameters":{"engine":"parallel"}}],"web_search_options":{}`, reserve: common.GetPointer(0.5), want: 1.53, wantSearches: 1, wantUse: ReserveAddOn},
		{name: "native searches beside the exa engine without a reserve stay closed", extra: `,"plugins":[{"id":"web","engine":"exa"}],"web_search_options":{}`, wantError: pricingdomain.ErrHostedSearchUnpriced.Error()},
		{name: "stop_server_tools_when with search stays closed", extra: tool + `}],"stop_server_tools_when":{"max_cost":1}`, wantError: "unpriced execution dimensions"},
		{name: "stop_server_tools_when without search stays unchecked", extra: `,"stop_server_tools_when":{"max_cost":1}`, want: 1.02},
		{name: "exa engine without a reserve stays closed", extra: `,"plugins":[{"id":"web","engine":"exa"}]`, wantError: pricingdomain.ErrHostedSearchUnpriced.Error()},
		{name: "zero max_uses", extra: tool + `,"parameters":{"max_uses":0}}]`, wantError: "invalid OpenRouter max_uses"},
		{name: "negative max_uses", extra: tool + `,"parameters":{"max_uses":-1}}]`, wantError: "invalid OpenRouter max_uses"},
		{name: "fractional max_uses", extra: tool + `,"parameters":{"max_uses":2.5}}]`, wantError: "invalid OpenRouter max_uses"},
		{name: "string max_uses", extra: tool + `,"parameters":{"max_uses":"3"}}]`, wantError: "invalid OpenRouter max_uses"},
		{name: "overflowing max_uses", extra: tool + `,"parameters":{"max_uses":18446744073709551616}}]`, wantError: "invalid OpenRouter max_uses"},
		{name: "zero max_tool_calls with a server tool", extra: tool + `}],"max_tool_calls":0`, wantError: "invalid OpenRouter max_tool_calls"},
		{name: "fractional max_tool_calls with a server tool", extra: tool + `}],"max_tool_calls":1.5`, wantError: "invalid OpenRouter max_tool_calls"},
		{name: "other plugin stays closed", extra: `,"plugins":[{"id":"web"},{"id":"file-parser"}]`, wantError: "unpriced execution dimensions"},
		{name: "other hosted tool stays closed", extra: `,"tools":[{"type":"web_search"}]`, wantError: "hosted tool pricing is unavailable"},
		{name: "online model stays closed", extra: `,"plugins":[{"id":"web"}],"models":["test/model:online"]`, wantError: "online routing includes unpriced hosted search"},
		{name: "Batch stays closed", extra: `,"plugins":[{"id":"web"}]`, operation: "batch_chat/completions", reserve: common.GetPointer(0.5), wantError: "hosted search is unavailable for Batch"},
		{name: "embeddings plugin stays closed", extra: `,"input":"text","plugins":[{"id":"web"}]`, operation: "embeddings", wantError: "unpriced execution dimensions"},
		{name: "embeddings server tool stays closed", extra: `,"input":"text"` + tool + `}]`, operation: "embeddings", wantError: "hosted tool pricing is unavailable"},
		{name: "embeddings web_search_options stays closed", extra: `,"input":"text","web_search_options":{}`, operation: "embeddings", wantError: pricingdomain.ErrHostedSearchUnpriced.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "web_search": json.RawMessage(`"0.01"`)}
			if tc.searchAbsent {
				delete(pricing, "web_search")
			}
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", ModelID: "test/model", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: pricing}}
			estimator.Models["test/model:batch"] = estimator.Models["test/model"]
			estimator.Models["test/model:online"] = estimator.Models["test/model"]
			estimator.Models["test/other"] = []PricedEndpoint{{Tag: "other", ModelID: "test/other", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`)}}}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			if tc.reserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/model": *tc.reserve}
			}
			info.SetEstimatePromptTokens(1000)
			extra := tc.extra
			if !strings.Contains(extra, "max_output_tokens") && tc.operation != "embeddings" {
				extra = tokens + extra
			}
			if tc.otherModel {
				extra += `,"models":["test/other"]`
			}
			operation := tc.operation
			if operation == "" {
				operation = "chat/completions"
			}
			cost, err := estimator.Estimate(info, []byte(`{"model":"test/model"`+extra+`}`), operation)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
			assert.Equal(t, tc.wantUse, estimator.ReserveUse)
			var wantQuantities map[pricingdomain.ReserveQuantity]int
			if tc.wantSearches > 0 {
				wantQuantities = map[pricingdomain.ReserveQuantity]int{pricingdomain.ReserveQuantitySearches: tc.wantSearches}
			}
			assert.Equal(t, wantQuantities, estimator.ReserveQuantities)
		})
	}
}

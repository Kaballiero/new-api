package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	usagegateway "github.com/QuantumNous/new-api/relay/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The Chat and Responses handlers forward OpenRouter plugins and server tool
// fields to an OpenRouter channel, whose reserve then counts the web searches
// recorded as openrouter_reservation_quantities, and strip them for other
// channels. The upstream answers the paid POST with an error, so the handler
// stops before settlement.
func TestHandlersForwardOpenRouterServerToolFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const chat = `{"model":"test/model","messages":[{"role":"user","content":"hello"}],"max_tokens":8`
	const responses = `{"model":"test/model","input":"hello","max_output_tokens":8`
	const searchFields = `,"plugins":[{"id":"web"}],"max_tool_calls":2,"stop_server_tools_when":{"max_cost":1}`
	for _, tc := range []struct {
		name, body  string
		responses   bool
		channelType int
		// forwarded and stripped are paths of the body the upstream receives.
		forwarded, stripped []string
		wantSearches        int
		wantError           string
	}{
		{name: "chat web plugin", body: chat + `,"plugins":[{"id":"web"}]}`, channelType: constant.ChannelTypeOpenRouter, forwarded: []string{"plugins"}, wantSearches: 1},
		{name: "chat server tool", body: chat + `,"tools":[{"type":"openrouter:web_search","parameters":{"max_uses":3}}],"max_tool_calls":2}`, channelType: constant.ChannelTypeOpenRouter, forwarded: []string{"tools.0.parameters", "max_tool_calls"}, wantSearches: 2},
		{name: "chat stop rules with search refused before the paid POST", body: chat + searchFields + `}`, channelType: constant.ChannelTypeOpenRouter, wantError: "unpriced execution dimensions"},
		{name: "responses web plugin", body: responses + `,"plugins":[{"id":"web"}]}`, responses: true, channelType: constant.ChannelTypeOpenRouter, forwarded: []string{"plugins"}, wantSearches: 1},
		{name: "chat to another channel", body: chat + searchFields + `}`, channelType: constant.ChannelTypeOpenAI, stripped: []string{"plugins", "max_tool_calls", "stop_server_tools_when"}},
		{name: "responses to another channel", body: responses + searchFields + `}`, responses: true, channelType: constant.ChannelTypeOpenAI, forwarded: []string{"max_tool_calls"}, stripped: []string{"plugins", "stop_server_tools_when"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					assert.Equal(t, "/v1/models/test/model/endpoints", r.URL.Path)
					_, _ = io.WriteString(w, `{"data":{"id":"test/model","endpoints":[{"tag":"provider","context_length":128000,"max_completion_tokens":128,"pricing":{"prompt":"0","completion":"0.001","web_search":"0.01"}}]}}`)
					return
				}
				var err error
				sent, err = io.ReadAll(r.Body)
				require.NoError(t, err)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"stop before settlement","type":"invalid_request_error"}}`)
			}))
			defer server.Close()

			path := "/v1/chat/completions"
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: relaytypes.RelayFormatOpenAI}
			var request dto.Request = &dto.GeneralOpenAIRequest{}
			if tc.responses {
				path = "/v1/responses"
				info = &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, RelayFormat: relaytypes.RelayFormatOpenAIResponses}
				request = &dto.OpenAIResponsesRequest{}
			}
			require.NoError(t, common.UnmarshalJsonStr(tc.body, request))
			info.Request = request
			info.OriginModelName = "test/model"
			info.RequestURLPath = path
			billing := &responsesRoutingBilling{}
			info.Billing = billing
			info.PriceData = hosttypes.PriceData{BillingGroupRatio: 1}

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyChannelType, tc.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "fixture-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "test/model")

			var apiErr *relaytypes.NewAPIError
			if tc.responses {
				apiErr = ResponsesHelper(c, info)
			} else {
				apiErr = TextHelper(c, info)
			}
			require.NotNil(t, apiErr)
			if tc.wantError != "" {
				assert.Contains(t, apiErr.Error(), tc.wantError)
				assert.Nil(t, sent)
				assert.Zero(t, billing.calls)
				return
			}
			assert.Contains(t, apiErr.Error(), "stop before settlement")
			require.NotNil(t, sent)
			for _, field := range tc.forwarded {
				assert.JSONEq(t, gjson.Get(tc.body, field).Raw, gjson.GetBytes(sent, field).Raw, field)
			}
			for _, field := range tc.stripped {
				assert.False(t, gjson.GetBytes(sent, field).Exists(), field)
			}
			if tc.channelType != constant.ChannelTypeOpenRouter {
				assert.Nil(t, info.OpenRouterCost)
				return
			}
			assert.Equal(t, 1, billing.calls)
			facts := usagegateway.Facts(c, info)
			require.NotNil(t, facts.OpenRouterCost)
			assert.Equal(t, map[string]int{"searches": tc.wantSearches}, facts.OpenRouterCost.ReserveQuantities)
		})
	}
}

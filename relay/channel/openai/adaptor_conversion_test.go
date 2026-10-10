package openai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiConversionRequestsSupportedStreamUsage(t *testing.T) {
	for _, tc := range []struct {
		name              string
		stream, supported bool
	}{{"supported stream", true, true}, {"unsupported stream", true, false}, {"nonstream", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, IsStream: tc.stream, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-test", SupportStreamOptions: tc.supported}}
			result, err := (&Adaptor{}).ConvertGeminiRequest(c, info, &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}}})
			require.NoError(t, err)
			chat, ok := result.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)
			if tc.stream && tc.supported {
				require.NotNil(t, chat.StreamOptions)
				assert.True(t, chat.StreamOptions.IncludeUsage)
			} else {
				assert.Nil(t, chat.StreamOptions)
			}
		})
	}
}

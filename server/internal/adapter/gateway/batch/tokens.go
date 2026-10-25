package batch

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	commonrelay "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func CountBatchItemTokens(c *gin.Context, modelName, endpoint string, body []byte) (int, error) {
	var request dto.Request
	info := &commonrelay.RelayInfo{OriginModelName: modelName, RelayMode: relayconstant.Path2RelayMode(endpoint)}
	switch endpoint {
	case "/v1/chat/completions":
		request, info.RelayFormat = &dto.GeneralOpenAIRequest{}, types.RelayFormatOpenAI
	case "/v1/responses":
		request, info.RelayFormat = &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAIResponses
	case "/v1/messages":
		request, info.RelayFormat = &dto.ClaudeRequest{}, types.RelayFormatClaude
		info.RelayMode = relayconstant.RelayModeChatCompletions
	case "/v1beta/generateContent":
		request, info.RelayFormat = &dto.GeminiChatRequest{}, types.RelayFormatGemini
		info.RelayMode = relayconstant.RelayModeGemini
	case "/v1/embeddings":
		request, info.RelayFormat = &dto.EmbeddingRequest{}, types.RelayFormatEmbedding
	default:
		return 0, errors.New("unsupported Batch endpoint")
	}
	if err := common.Unmarshal(body, request); err != nil {
		return 0, err
	}
	// Metadata extraction never re-marshals the prepared provider payload.
	itemContext := c.Copy()
	common.SetContextKey(itemContext, constant.ContextKeyOriginalModel, modelName)
	return service.CountRequestToken(itemContext, request.GetTokenCountMeta(), info)
}

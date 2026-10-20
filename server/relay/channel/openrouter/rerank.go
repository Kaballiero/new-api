package openrouter

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// RerankResponse preserves the upstream result while the existing durable Task
// owns cost settlement. Missing cost never authorizes a refund or another POST.
func RerankResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage *dto.Usage, apiErr *types.NewAPIError) {
	defer func() {
		if apiErr != nil && service.HasOpenRouterSyncTask(info) {
			_ = service.FailOpenRouterSyncTask(info, "invalid_rerank_result")
		}
	}()
	if resp == nil || resp.Body == nil {
		return nil, CostError("OpenRouter rerank response is missing")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil || len(body) == 0 || len(body) > 64<<20 {
		return nil, CostError("OpenRouter rerank response cannot be inspected")
	}
	var result struct {
		ID      string                      `json:"id"`
		Results *[]dto.RerankResponseResult `json:"results"`
	}
	if common.Unmarshal(body, &result) != nil || result.Results == nil {
		return nil, CostError("OpenRouter rerank response is invalid")
	}
	state := info.OpenRouterCost
	if result.ID != "" {
		if state.GenerationID != "" && state.GenerationID != result.ID {
			return nil, CostError("OpenRouter rerank generation identity changed")
		}
		state.GenerationID = result.ID
	}
	state.Observe(body, true)
	if state.UnknownReason != "" && state.UnknownReason != "missing_terminal_cost" {
		return nil, CostError("OpenRouter rerank usage.cost is invalid")
	}
	// Record the completion and financial target before delivering success.
	body, err = common.ProjectClientResponse(c, body)
	if err != nil {
		return nil, CostError("OpenRouter rerank response projection failed")
	}
	if err := service.CompleteOpenRouterSyncTask(c, info, body, "application/json"); err != nil {
		return nil, CostError(err.Error())
	}
	c.Header("X-Task-Id", state.TaskID)
	c.Data(http.StatusOK, "application/json", body)
	return &dto.Usage{Cost: state.CostUSD}, nil
}

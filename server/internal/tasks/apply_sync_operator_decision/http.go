package apply_sync_operator_decision

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/domain"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	execute func(context.Context, Input) (domain.OpenRouterSyncOperatorResult, error)
	audit   func(*gin.Context, string, string, map[string]any, map[string]any, int, bool)
	log     func(string)
}

func NewHandler(execute func(context.Context, Input) (domain.OpenRouterSyncOperatorResult, error), audit func(*gin.Context, string, string, map[string]any, map[string]any, int, bool), log func(string)) Handler {
	return Handler{execute, audit, log}
}
func (h Handler) Release(c *gin.Context) {
	h.handle(c, domain.OpenRouterSyncOperatorRelease, "openrouter_sync.release")
}
func (h Handler) Settle(c *gin.Context) {
	h.handle(c, domain.OpenRouterSyncOperatorSettle, "openrouter_sync.settle_cost")
}
func (h Handler) handle(c *gin.Context, action, auditAction string) {
	var request struct {
		CostUSD  string `json:"cost_usd"`
		Evidence string `json:"evidence"`
	}
	taskID := c.Param("task_id")
	result := domain.OpenRouterSyncOperatorResult{TaskID: taskID, Action: action}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	err := common.DecodeJson(c.Request.Body, &request)
	if err != nil {
		err = fmt.Errorf("%w: invalid request body", domain.ErrOpenRouterSyncOperatorInvalid)
	} else {
		result, err = h.execute(c.Request.Context(), Input{TaskID: taskID, Action: action, CostUSD: request.CostUSD, Evidence: request.Evidence, OperatorID: c.GetInt("id")})
	}
	switch {
	case err == nil:
		common.ApiSuccess(c, gin.H{"task_id": result.TaskID, "action": result.Action, "billing_state": result.BillingState, "reserved_quota": result.ReservedQuota, "target_quota": result.TargetQuota, "already_applied": result.AlreadyApplied})
	case errors.Is(err, domain.ErrOpenRouterSyncOperatorInvalid):
		common.ApiErrorMsgStatus(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrOpenRouterSyncOperatorConflict):
		common.ApiErrorMsgStatus(c, http.StatusConflict, err.Error())
	default:
		common.ApiErrorMsgStatus(c, http.StatusInternalServerError, "OpenRouter operator decision failed")
	}
	costUSD := result.CostUSD
	if costUSD == "" {
		costUSD = request.CostUSD
	}
	h.audit(c, auditAction, "OpenRouter synchronous task operator decision", nil, map[string]any{"task_id": taskID, "platform": result.Platform, "action": action, "cost_usd": costUSD, "reserved_quota": result.ReservedQuota, "target_quota": result.TargetQuota, "evidence": request.Evidence}, c.Writer.Status(), err == nil)
	outcome := "applied"
	if err != nil {
		outcome = err.Error()
	} else if result.AlreadyApplied {
		outcome = "already_applied"
	}
	h.log(fmt.Sprintf("OpenRouter sync operator decision: task_id=%s action=%s cost_usd=%q operator_id=%d outcome=%q", taskID, action, costUSD, c.GetInt("id"), outcome))
}

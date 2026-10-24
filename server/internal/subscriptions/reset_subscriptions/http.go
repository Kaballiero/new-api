package reset_subscriptions

import (
	"github.com/QuantumNous/new-api/common"
	auditdomain "github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/dto"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
	"strconv"
)

type AdminResetSubscriptionRequest struct {
	PlanID  int   `json:"plan_id"`
	Advance *bool `json:"advance_reset_time"`
}
type HTTP struct {
	usecase Usecase
	audit   ResetAudit
}

func NewHTTP(usecase Usecase, audit ResetAudit) HTTP { return HTTP{usecase, audit} }

type ResetAudit interface {
	RecordUserLogs(c *gin.Context, result *domain.ResetResult, operator *auditdomain.Operator)
	Manage(c *gin.Context, target int, action string, params map[string]any)
	Operator(c *gin.Context) *auditdomain.Operator
	PlanReset(result domain.ResetResult)
}

func (h HTTP) ForUser(c *gin.Context) { h.handle(c, true) }
func (h HTTP) ForPlan(c *gin.Context) { h.handle(c, false) }
func (h HTTP) handle(c *gin.Context, forUser bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		if forUser {
			transport.WriteError(c, domain.ErrInvalidUserID)
		} else {
			transport.WriteError(c, domain.ErrInvalidID)
		}
		return
	}
	var req AdminResetSubscriptionRequest
	if c.ShouldBindJSON(&req) != nil || (forUser && req.PlanID <= 0) {
		transport.InvalidParameters(c)
		return
	}
	advance := true
	if req.Advance != nil {
		advance = *req.Advance
	}
	in := Input{PlanID: id, AdvanceResetTime: advance}
	if forUser {
		in.UserID = id
		in.PlanID = req.PlanID
	}
	result, err := h.usecase.Execute(in)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	h.audit.RecordUserLogs(c, &result, h.audit.Operator(c))
	params := map[string]any{"plan_id": result.PlanId, "plan_title": result.PlanTitle, "reset_count": result.ResetCount, "user_count": result.UserCount, "advance_reset_time": result.AdvanceResetTime}
	if forUser {
		params["target_user_id"] = id
		h.audit.Manage(c, id, "subscription.user_plan_reset", params)
	} else {
		h.audit.PlanReset(result)
		h.audit.Manage(c, c.GetInt("id"), "subscription.plan_reset", params)
	}
	common.ApiSuccess(c, dto.SubscriptionResetResult(result))
}

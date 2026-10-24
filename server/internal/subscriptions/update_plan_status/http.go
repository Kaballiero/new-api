package update_plan_status

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type AdminUpdateSubscriptionPlanStatusRequest struct {
	Enabled *bool `json:"enabled"`
}
type HTTP struct {
	usecase    Usecase
	compliance func() bool
}

func NewHTTP(usecase Usecase, compliance func() bool) HTTP {
	return HTTP{usecase: usecase, compliance: compliance}
}
func (h HTTP) Handle(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		transport.WriteError(c, domain.ErrInvalidID)
		return
	}
	var req AdminUpdateSubscriptionPlanStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		transport.InvalidParameters(c)
		return
	}
	if err := h.usecase.Execute(Input{PlanID: id, Enabled: *req.Enabled}); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

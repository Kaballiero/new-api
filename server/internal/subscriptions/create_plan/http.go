package create_plan

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"github.com/QuantumNous/new-api/internal/subscriptions/dto"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
)

type Request struct {
	Plan dto.Plan `json:"plan"`
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

	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
		transport.InvalidParameters(c)
		return
	}
	result, err := h.usecase.Execute(domain.SubscriptionPlan(req.Plan))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.Plan(result))
}

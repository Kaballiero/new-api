package purchase_with_balance

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase    Usecase
	compliance func() bool
}

func NewHTTP(u Usecase, compliance func() bool) HTTP { return HTTP{u, compliance} }
func (h HTTP) Handle(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	var req struct {
		PlanID int `json:"plan_id"`
	}
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	if err := h.usecase.Execute(c.GetInt("id"), req.PlanID); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

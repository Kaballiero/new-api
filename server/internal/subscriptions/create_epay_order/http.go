package create_epay_order

import (
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
)

type SubscriptionEpayPayRequest struct {
	PlanID        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method"`
}
type HTTP struct {
	usecase    Usecase
	compliance func() bool
}

func NewHTTP(u Usecase, compliance func() bool) HTTP { return HTTP{u, compliance} }
func (h HTTP) Handle(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	var req SubscriptionEpayPayRequest
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	result, err := h.usecase.Execute(Input{UserID: c.GetInt("id"), PlanID: req.PlanID, PaymentMethod: req.PaymentMethod})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "success", "data": result.Params, "url": result.URL})
}

package create_stripe_order

import (
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
	"github.com/gin-gonic/gin"
)

type SubscriptionStripePayRequest struct {
	PlanID int `json:"plan_id"`
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
	var req SubscriptionStripePayRequest
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	link, err := h.usecase.Execute(c.Request.Context(), c.GetInt("id"), req.PlanID)
	if err != nil {
		transport.CheckoutError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "success", "data": gin.H{"pay_link": link}})
}

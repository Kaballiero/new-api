package create_pancake_order

import (
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"

	"github.com/gin-gonic/gin"
)

type SubscriptionWaffoPancakePayRequest struct {
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
	var req SubscriptionWaffoPancakePayRequest
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		transport.InvalidParameters(c)
		return
	}
	result, err := h.usecase.Execute(c.Request.Context(), Input{UserID: c.GetInt("id"), PlanID: req.PlanID})
	if err != nil {
		transport.CheckoutError(c, err)
		return
	}
	c.JSON(
		200,
		gin.H{
			"message": "success",
			"data": gin.H{
				"checkout_url":     result.Session.CheckoutURL,
				"session_id":       result.Session.SessionID,
				"expires_at":       result.Session.ExpiresAt,
				"order_id":         result.OrderID,
				"token":            result.Session.Token,
				"token_expires_at": result.Session.TokenExpiresAt,
			},
		},
	)
}

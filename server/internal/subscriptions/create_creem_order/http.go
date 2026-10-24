package create_creem_order

import (
	"bytes"
	"io"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type SubscriptionCreemPayRequest struct {
	PlanID int `json:"plan_id"`
}
type HTTP struct {
	usecase     Usecase
	compliance  func() bool
	diagnostics Diagnostics
}

func NewHTTP(u Usecase, compliance func() bool, diagnostics Diagnostics) HTTP {
	return HTTP{u, compliance, diagnostics}
}
func (h HTTP) Handle(c *gin.Context) {
	if !transport.CheckCompliance(c, h.compliance) {
		return
	}
	var req SubscriptionCreemPayRequest
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.diagnostics.Error(c.Request.Context(), "Creem subscription payment request read failed error=%q", err.Error())
		c.JSON(200, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgSubscriptionsReadQuery)})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if c.ShouldBindJSON(&req) != nil || req.PlanID <= 0 {
		c.JSON(200, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgSubscriptionsInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(c.Request.Context(), Input{UserID: c.GetInt("id"), PlanID: req.PlanID})
	if err != nil {
		transport.CheckoutError(c, err)
		return
	}
	c.JSON(200, gin.H{"message": "success", "data": gin.H{"checkout_url": result.CheckoutURL, "order_id": result.OrderID}})
}

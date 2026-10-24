package create_pancake_product

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type createWaffoPancakeSubscriptionProductRequest struct {
	Name   string `json:"name"`
	Amount string `json:"amount"`
}
type HTTP struct{ usecase Usecase }

func NewHTTP(u Usecase) HTTP { return HTTP{u} }
func (h HTTP) Handle(c *gin.Context) {
	var req createWaffoPancakeSubscriptionProductRequest
	if c.Request.ContentLength > 0 {
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(200, gin.H{"message": "error", "data": i18n.T(c, i18n.MsgSubscriptionsInvalidParameters)})
			return
		}
	}
	result, err := h.usecase.Execute(c.Request.Context(), Input{Name: req.Name, Amount: req.Amount})
	if err != nil {
		c.JSON(200, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(
		200,
		gin.H{
			"message": "success",
			"data": gin.H{
				"product_id":   result.ProductID,
				"product_name": result.ProductName,
				"store_id":     result.StoreID,
			},
		},
	)
}

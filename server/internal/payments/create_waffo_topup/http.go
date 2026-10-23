package create_waffo_topup

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct {
	usecase Usecase
	enabled func() bool
}

func NewHTTP(usecase Usecase, enabled func() bool) HTTP {
	return HTTP{usecase: usecase, enabled: enabled}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrWaffoDisabled)})
		return
	}
	var req dto.WaffoPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(
		transport.Context(c),
		Input{UserID: c.GetInt("id"), Amount: req.Amount, Index: req.PayMethodIndex, MethodType: req.PayMethodType, MethodName: req.PayMethodName},
	)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"payment_url": result.PaymentURL, "order_id": result.OrderID}})
}

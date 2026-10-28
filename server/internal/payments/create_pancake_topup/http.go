package create_pancake_topup

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
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
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrPancakeConfig)})
		return
	}
	var req dto.WaffoPancakePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{UserID: c.GetInt("id"), Amount: req.Amount})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(
		http.StatusOK,
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

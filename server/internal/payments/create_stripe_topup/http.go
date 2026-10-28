package create_stripe_topup

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	var req dto.StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	link, err := h.usecase.Execute(
		transport.Context(c),
		Input{UserID: c.GetInt("id"), Amount: req.Amount, PaymentMethod: req.PaymentMethod, SuccessURL: req.SuccessURL, CancelURL: req.CancelURL},
	)
	if err != nil {
		var minimum domain.MinimumAmount
		switch {
		case errors.As(err, &minimum), errors.Is(err, domain.ErrStripeMaximum):
			c.JSON(http.StatusOK, gin.H{"message": transport.Message(c, err), "data": 10})
		case errors.Is(err, domain.ErrSuccessURL), errors.Is(err, domain.ErrCancelURL):
			c.JSON(http.StatusBadRequest, gin.H{"message": transport.Message(c, err), "data": ""})
		default:
			c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"pay_link": link}})
}

package create_epay_topup

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct {
	usecase Usecase
	created func(c *gin.Context, req dto.EpayRequest, result Result)
}

func NewHTTP(usecase Usecase, created func(c *gin.Context, req dto.EpayRequest, result Result)) HTTP {
	return HTTP{usecase: usecase, created: created}
}

func (h HTTP) Handle(c *gin.Context) {
	var req dto.EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{UserID: c.GetInt("id"), Amount: req.Amount, PaymentMethod: req.PaymentMethod})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	h.created(c, req, result)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": result.Params, "url": result.URL})
}

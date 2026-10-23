package quote_waffo_topup

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	var req dto.WaffoPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	money, err := h.usecase.Execute(Input{UserID: c.GetInt("id"), Amount: req.Amount})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatFloat(money, 'f', 2, 64)})
}

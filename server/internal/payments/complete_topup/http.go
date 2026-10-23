package complete_topup

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }

func (h HTTP) Handle(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		transport.WriteError(c, domain.ErrInvalidParameters)
		return
	}
	if err := h.usecase.Execute(Input{TradeNo: req.TradeNo, CallerIP: c.ClientIP()}); err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

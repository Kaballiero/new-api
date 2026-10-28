package complete_topup

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
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

package create_creem_topup

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct {
	usecase Usecase
	observe func(c *gin.Context, payload []byte)
}

func NewHTTP(usecase Usecase, observe func(c *gin.Context, payload []byte)) HTTP {
	return HTTP{usecase: usecase, observe: observe}
}

func (h HTTP) Handle(c *gin.Context) {
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "Creem 支付请求读取失败 error=%q", err)
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrReadQuery)})
		return
	}
	h.observe(c, payload)
	c.Request.Body = io.NopCloser(bytes.NewReader(payload))
	var req CreemPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, domain.ErrInvalidParameters)})
		return
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{UserID: c.GetInt("id"), ProductID: req.ProductId, PaymentMethod: req.PaymentMethod})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": transport.Message(c, err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"checkout_url": result.CheckoutURL, "order_id": result.OrderID}})
}

package receive_creem_webhook

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
)

type HTTP struct {
	usecase Usecase
	enabled func() bool
	observe func(c *gin.Context, payload []byte, signature string)
}

func NewHTTP(usecase Usecase, enabled func() bool, observe func(c *gin.Context, payload []byte, signature string)) HTTP {
	return HTTP{usecase: usecase, enabled: enabled, observe: observe}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		h.usecase.diagnostics.Warn(transport.Context(c), "Creem webhook rejected reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP())
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "Creem webhook request body read failed path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	h.observe(c, payload, c.GetHeader("creem-signature"))
	err = h.usecase.Execute(transport.Context(c), Input{Payload: payload, Signature: c.GetHeader("creem-signature"), CallerIP: c.ClientIP(), Path: c.Request.RequestURI})
	switch {
	case err == nil:
		c.Status(http.StatusOK)
	case errors.Is(err, domain.ErrCreemSignature):
		c.AbortWithStatus(http.StatusUnauthorized)
	case errors.Is(err, domain.ErrCreemPayload), errors.Is(err, domain.ErrPaymentTradeNo), errors.Is(err, domain.ErrOrderMissing):
		c.AbortWithStatus(http.StatusBadRequest)
	default:
		c.AbortWithStatus(http.StatusInternalServerError)
	}
}

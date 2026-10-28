package receive_pancake_webhook

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
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
		h.usecase.diagnostics.Warn(transport.Context(c), "Waffo Pancake webhook rejected reason=webhook_disabled path=%q client_ip=%s", c.Request.URL.Path, c.ClientIP())
		c.String(http.StatusForbidden, "webhook disabled")
		return
	}
	environment := strings.TrimSpace(c.Param("env"))
	if environment != "test" && environment != "prod" {
		h.usecase.diagnostics.Warn(transport.Context(c), "Waffo Pancake webhook path environment segment is invalid env=%q path=%q client_ip=%s", environment, c.Request.URL.Path, c.ClientIP())
		c.String(http.StatusNotFound, "unknown env")
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "Waffo Pancake webhook request body read failed path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err)
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	h.observe(c, payload, c.GetHeader("X-Waffo-Signature"))
	err = h.usecase.Execute(transport.Context(c), Input{Payload: string(payload), Signature: c.GetHeader("X-Waffo-Signature"), Environment: environment, CallerIP: c.ClientIP(), Path: c.Request.URL.Path})
	switch {
	case err == nil:
		c.String(http.StatusOK, "OK")
	case errors.Is(err, domain.ErrPancakeSignature):
		c.String(http.StatusUnauthorized, "invalid signature")
	default:
		c.String(http.StatusInternalServerError, "retry")
	}
}

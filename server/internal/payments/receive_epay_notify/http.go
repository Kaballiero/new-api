package receive_epay_notify

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase Usecase
	enabled func() bool
	observe func(c *gin.Context, params map[string]string)
}

func NewHTTP(usecase Usecase, enabled func() bool, observe func(c *gin.Context, params map[string]string)) HTTP {
	return HTTP{usecase: usecase, enabled: enabled, observe: observe}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		h.usecase.diagnostics.Warn(transport.Context(c), "Epay webhook rejected reason=webhook_disabled path=%q client_ip=%s", c.Request.URL.Path, c.ClientIP())
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	values := c.Request.URL.Query()
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			h.usecase.diagnostics.Error(transport.Context(c), "Epay webhook POST form parsing failed path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), err)
			_, _ = c.Writer.Write([]byte("fail"))
			return
		}
		values = c.Request.PostForm
	}
	params := make(map[string]string, len(values))
	for key := range values {
		params[key] = values.Get(key)
	}
	h.observe(c, params)
	result, err := h.usecase.Execute(transport.Context(c), Input{Params: params, CallerIP: c.ClientIP(), Path: c.Request.URL.Path})
	if err != nil {
		_, writeErr := c.Writer.Write([]byte("fail"))
		var signatureFailure SignatureFailure
		switch {
		case errors.As(err, &signatureFailure):
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "Epay webhook response write failed path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), writeErr)
			}
			if signatureFailure.Cause != nil {
				h.usecase.diagnostics.Warn(transport.Context(c), "Epay webhook signature verification failed path=%q client_ip=%s verify_error=%q", c.Request.URL.Path, c.ClientIP(), signatureFailure.Cause)
			} else {
				h.usecase.diagnostics.Warn(transport.Context(c), "Epay webhook signature verification failed path=%q client_ip=%s verify_status=false", c.Request.URL.Path, c.ClientIP())
			}
		case errors.Is(err, domain.ErrEpayConfig):
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "Epay webhook response write failed path=%q client_ip=%s error=%q", c.Request.URL.Path, c.ClientIP(), writeErr)
			}
		case result.Event.Verified:
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "Epay webhook response write failed trade_no=%s client_ip=%s error=%q", result.Event.TradeNo, c.ClientIP(), writeErr)
			}
		}
		return
	}
	if _, writeErr := c.Writer.Write([]byte("success")); writeErr != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "Epay webhook response write failed trade_no=%s client_ip=%s error=%q", result.Event.TradeNo, c.ClientIP(), writeErr)
	}
}

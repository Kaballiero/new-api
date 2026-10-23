package receive_epay_notify

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/internal/payments/transport"
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
		h.usecase.diagnostics.Warn(transport.Context(c), "易支付 webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP())
		_, _ = c.Writer.Write([]byte("fail"))
		return
	}
	values := c.Request.URL.Query()
	if c.Request.Method == "POST" {
		if err := c.Request.ParseForm(); err != nil {
			h.usecase.diagnostics.Error(transport.Context(c), "易支付 webhook POST 表单解析失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err)
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
	result, err := h.usecase.Execute(transport.Context(c), Input{Params: params, CallerIP: c.ClientIP(), Path: c.Request.RequestURI})
	if err != nil {
		_, writeErr := c.Writer.Write([]byte("fail"))
		var signatureFailure SignatureFailure
		switch {
		case errors.As(err, &signatureFailure):
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), writeErr)
			}
			if signatureFailure.Cause != nil {
				h.usecase.diagnostics.Warn(transport.Context(c), "易支付 webhook 验签失败 path=%q client_ip=%s verify_error=%q", c.Request.RequestURI, c.ClientIP(), signatureFailure.Cause)
			} else {
				h.usecase.diagnostics.Warn(transport.Context(c), "易支付 webhook 验签失败 path=%q client_ip=%s verify_status=false", c.Request.RequestURI, c.ClientIP())
			}
		case errors.Is(err, domain.ErrEpayConfig):
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "易支付 webhook 响应写入失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), writeErr)
			}
		case result.Event.Verified:
			if writeErr != nil {
				h.usecase.diagnostics.Error(transport.Context(c), "易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", result.Event.TradeNo, c.ClientIP(), writeErr)
			}
		}
		return
	}
	if _, writeErr := c.Writer.Write([]byte("success")); writeErr != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "易支付 webhook 响应写入失败 trade_no=%s client_ip=%s error=%q", result.Event.TradeNo, c.ClientIP(), writeErr)
	}
}

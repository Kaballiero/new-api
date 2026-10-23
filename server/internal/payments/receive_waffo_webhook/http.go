package receive_waffo_webhook

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
}

func NewHTTP(usecase Usecase, enabled func() bool) HTTP {
	return HTTP{usecase: usecase, enabled: enabled}
}

func (h HTTP) Handle(c *gin.Context) {
	if !h.enabled() {
		h.usecase.diagnostics.Warn(transport.Context(c), "Waffo webhook 被拒绝 reason=webhook_disabled path=%q client_ip=%s", c.Request.RequestURI, c.ClientIP())
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	payload, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.usecase.diagnostics.Error(transport.Context(c), "Waffo webhook 读取请求体失败 path=%q client_ip=%s error=%q", c.Request.RequestURI, c.ClientIP(), err)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	result, err := h.usecase.Execute(transport.Context(c), Input{Payload: payload, Signature: c.GetHeader("X-SIGNATURE"), CallerIP: c.ClientIP(), Path: c.Request.RequestURI})
	if err != nil {
		if errors.Is(err, domain.ErrPaymentConfig) {
			c.AbortWithStatus(http.StatusInternalServerError)
		} else {
			c.AbortWithStatus(http.StatusBadRequest)
		}
		return
	}
	c.Header("X-SIGNATURE", result.Signature)
	c.Data(http.StatusOK, "application/json", []byte(result.Body))
}

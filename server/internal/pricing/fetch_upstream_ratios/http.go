package fetch_upstream_ratios

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/pricing/transport"
	"github.com/gin-gonic/gin"
)

type HTTP struct {
	usecase Usecase
	log     func(string)
	decode  func(func(any) error) (Request, error)
}

func NewHTTP(usecase Usecase, log func(string), decode func(func(any) error) (Request, error)) HTTP {
	return HTTP{usecase: usecase, log: log, decode: decode}
}
func (h HTTP) Handle(c *gin.Context) {
	req, err := h.decode(c.ShouldBindJSON)
	if err != nil {
		h.log("failed to bind upstream request: " + err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgPricingRequestInvalid)})
		return
	}
	errorText := func(err error) string { return transport.UpstreamErrorText(c, err) }
	out, err := h.usecase.Execute(c.Request.Context(), Input{Request: req, ErrorText: errorText})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

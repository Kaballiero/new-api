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
}

func NewHTTP(usecase Usecase, log func(string)) HTTP { return HTTP{usecase: usecase, log: log} }
func (h HTTP) Handle(c *gin.Context) {
	var req Request
	if err := c.ShouldBindJSON(&req); err != nil {
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

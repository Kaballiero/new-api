package fetch_upstream_ratios

import (
	"net/http"

	"github.com/gin-gonic/gin"

	pricinghttp "github.com/QuantumNous/new-api/internal/adapter/http/pricing"
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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "请求参数格式错误"})
		return
	}
	out, err := h.usecase.Execute(c.Request.Context(), req)
	if err != nil {
		if err == ErrNoUpstreams {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		pricinghttp.WriteFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}

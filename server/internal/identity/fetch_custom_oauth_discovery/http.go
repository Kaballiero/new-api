package fetch_custom_oauth_discovery

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) *HTTP { return &HTTP{usecase: usecase} }
func (h *HTTP) Handle(c *gin.Context) {
	var req FetchCustomOAuthDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	response, err := h.usecase.Execute(c.Request.Context(), req)
	if err != nil {
		transport.WriteCustomOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"well_known_url": response.WellKnownURL, "discovery": response.Discovery}})
}

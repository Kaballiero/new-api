package update_custom_oauth_provider

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) *HTTP { return &HTTP{usecase: usecase} }
func (h *HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "无效的 ID")
		return
	}
	var req UpdateCustomOAuthProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "无效的请求参数: "+err.Error())
		return
	}
	provider, err := h.usecase.Execute(Input{ID: id, Request: req})
	if err != nil {
		transport.WriteCustomOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "更新成功", "data": dto.CustomOAuthProviderResponseFrom(provider)})
}

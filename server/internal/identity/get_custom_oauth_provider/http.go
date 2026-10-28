package get_custom_oauth_provider

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
	provider, err := h.usecase.Execute(id)
	if err != nil {
		transport.WriteCustomOAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": dto.CustomOAuthProviderResponseFrom(provider)})
}

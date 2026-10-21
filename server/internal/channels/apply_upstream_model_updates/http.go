package apply_upstream_model_updates

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	output, err := h.usecase(c).Execute(input)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}

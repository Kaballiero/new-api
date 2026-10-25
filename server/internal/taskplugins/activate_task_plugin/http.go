package activate_task_plugin

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type taskPluginActivateRequest struct {
	Version string `json:"version" binding:"required"`
}
type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	var request taskPluginActivateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	input := Input{Version: request.Version}
	input.Key = c.Param("key")
	value, err := h.usecase.Execute(transport.Context(c), input)
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}

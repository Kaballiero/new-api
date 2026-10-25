package get_task_plugin

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	input := Input{}
	input.Key = c.Param("key")
	input.VersionQuery = c.Query("version")
	value, err := h.usecase.Execute(transport.Context(c), input)
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}

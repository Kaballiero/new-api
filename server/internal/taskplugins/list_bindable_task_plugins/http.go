package list_bindable_task_plugins

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	value, err := h.usecase.Execute(transport.Context(c))
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}

package dryrun_task_plugin

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type taskPluginDryRunRequest struct {
	Hook   string            `json:"hook" binding:"required"`
	Member string            `json:"member"`
	Args   []json.RawMessage `json:"args"`
}
type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	var request taskPluginDryRunRequest
	if e := c.ShouldBindJSON(&request); e != nil {
		common.ApiErrorMsg(c, e.Error())
		return
	}
	v, e := h.usecase.Execute(transport.Context(c), Input{Key: c.Param("key"), Hook: request.Hook, Member: request.Member, Args: request.Args})
	if e != nil {
		transport.Error(c, e)
		return
	}
	common.ApiSuccess(c, v)
}

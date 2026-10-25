package set_task_plugin_status

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/taskplugins/transport"
	"github.com/gin-gonic/gin"
)

type taskPluginStatusRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}
type Handler struct{ usecase Usecase }

func NewHandler(u Usecase) Handler { return Handler{usecase: u} }
func (h Handler) ServeHTTP(c *gin.Context) {
	var request taskPluginStatusRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		common.ApiErrorMsg(c, i18n.Translate(transport.Language(c), i18n.MsgTaskPluginEnabledRequired))
		return
	}
	input := Input{Enabled: request.Enabled}
	input.Key = c.Param("key")
	input.Force = c.Query("force") == "true"
	input.Cascade = c.Query("cascade") == "true"
	value, err := h.usecase.Execute(transport.Context(c), input)
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, value)
}

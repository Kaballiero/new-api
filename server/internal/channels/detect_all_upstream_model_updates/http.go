package detect_all_upstream_model_updates

import (
	"net/http"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	output, err := h.usecase(c).Execute()
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	if !output.Created {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": i18n.T(c, i18n.MsgChannelModelUpdateTaskBusy), "data": gin.H{"task_id": output.Task.ID, "status": output.Task.Status, "type": output.Task.Type}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"task_id": output.Task.ID, "status": output.Task.Status}})
}

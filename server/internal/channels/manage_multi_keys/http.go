package manage_multi_keys

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/transport"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }

// outcomeMessages phrases the completed actions; listing the key status has no
// message.
var outcomeMessages = map[Outcome]string{
	KeyDisabled:             i18n.MsgChannelKeyDisabled,
	KeyEnabled:              i18n.MsgChannelKeyEnabled,
	KeysEnabled:             i18n.MsgChannelKeysEnabled,
	KeysDisabled:            i18n.MsgChannelKeysDisabled,
	KeyDeleted:              i18n.MsgChannelKeyDeleted,
	AutoDisabledKeysDeleted: i18n.MsgChannelAutoDisabledKeysDeleted,
}

func (h HTTP) Handle(c *gin.Context) {
	var request MultiKeyManageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	output, err := h.usecase(c).Execute(request)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	message := ""
	if key, ok := outcomeMessages[output.Outcome]; ok {
		message = i18n.T(c, key, map[string]any{"Count": output.Count})
	}
	response := gin.H{"success": true, "message": message}
	if output.Data != nil {
		response["data"] = output.Data
	}
	c.JSON(http.StatusOK, response)
}

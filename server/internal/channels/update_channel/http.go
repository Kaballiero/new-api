package update_channel

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ usecase func(*gin.Context) Usecase }

func NewHTTP(usecase func(*gin.Context) Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	var request PatchChannel
	raw, err := c.GetRawData()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := common.Unmarshal(raw, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	var fields map[string]any
	if err := common.Unmarshal(raw, &fields); err != nil {
		common.ApiError(c, err)
		return
	}
	output, err := h.usecase(c).Execute(Input{Request: request, Fields: fields, Raw: raw})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, output)
}

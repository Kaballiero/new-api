package update_channel_status_batch

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	store   Store
	refresh func()
	audit   func(Input, int)
}

func NewHTTP(store Store, refresh func(), audit func(Input, int)) HTTP {
	return HTTP{store: store, refresh: refresh, audit: audit}
}
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store, h.refresh, h.audit)
	_ = scenario
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Ids) == 0 || (input.Status != common.ChannelStatusEnabled && input.Status != common.ChannelStatusManuallyDisabled) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	changed := scenario.Execute(input)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": changed})
}

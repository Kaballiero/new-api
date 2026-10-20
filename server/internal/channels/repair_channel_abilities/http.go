package repair_channel_abilities

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ store Store }

func NewHTTP(store Store) HTTP { return HTTP{store: store} }
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.store)
	_ = scenario
	output, err := scenario.Execute()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"success": output.Success, "fails": output.Fails}})
}

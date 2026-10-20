package get_affiliate_code

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) *HTTP { return &HTTP{scenario: scenario} }
func (h *HTTP) Handle(c *gin.Context) {
	id := c.GetInt("id")
	code, failure := h.scenario.Execute(Input{ID: id})
	if failure != nil {
		if !failure.Saving {
			common.ApiError(c, failure.Err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": code})
	return
}

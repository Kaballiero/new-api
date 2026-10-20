package detect_upstream_model_updates

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiError(c, err)
		return
	}
	output, failure := h.scenario.Execute(input)
	if failure != nil {
		if failure.Kind == "invalid" {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		} else {
			common.ApiError(c, failure.Cause)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}

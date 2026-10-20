package copy_channel

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid id"})
		return
	}

	suffix := c.DefaultQuery("suffix", "_复制")
	resetBalance := true
	if rbStr := c.DefaultQuery("reset_balance", "true"); rbStr != "" {
		if v, err := strconv.ParseBool(rbStr); err == nil {
			resetBalance = v
		}
	}

	output, failure := h.scenario.Execute(Input{ID: id, Suffix: suffix, ResetBalance: resetBalance})
	if failure != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": gin.H{"id": output.ID}})
}

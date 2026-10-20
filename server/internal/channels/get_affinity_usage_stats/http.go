package get_affinity_usage_stats

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	output, err := h.scenario.Execute(Input{Rule: strings.TrimSpace(c.Query("rule_name")), Group: strings.TrimSpace(c.Query("using_group")), Key: strings.TrimSpace(c.Query("key_fp"))})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": output})
}

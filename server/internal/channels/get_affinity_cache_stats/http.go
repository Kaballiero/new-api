package get_affinity_cache_stats

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": h.scenario.Execute()})
}

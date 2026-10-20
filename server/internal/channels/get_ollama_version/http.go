package get_ollama_version

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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid channel id"})
		return
	}
	output, failure := h.scenario.Execute(Input{ID: id})
	if failure != nil {
		status := http.StatusBadRequest
		if failure.Kind == "missing" {
			status = http.StatusNotFound
		} else if failure.Kind == "provider" {
			status = http.StatusOK
		}
		c.JSON(status, gin.H{"success": false, "message": failure.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"version": output.Version}})
}

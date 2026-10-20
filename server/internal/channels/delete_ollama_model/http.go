package delete_ollama_model

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	var input Input
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request parameters"})
		return
	}
	output, failure := h.scenario.Execute(input)
	if failure != nil {
		status := http.StatusBadRequest
		if failure.Kind == "missing" {
			status = http.StatusNotFound
		} else if failure.Kind == "provider" {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"success": false, "message": failure.Message})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": output.Message})
}

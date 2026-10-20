package list_channel_models

import "github.com/gin-gonic/gin"

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	c.JSON(200, gin.H{"success": true, "data": h.scenario.Execute()})
}

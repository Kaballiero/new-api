package get_groups

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) *HTTP { return &HTTP{scenario: scenario} }
func (h *HTTP) List(c *gin.Context) {
	groupNames := h.scenario.Execute()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": groupNames})
}

func (h *HTTP) ForUser(c *gin.Context) {
	userId := c.GetInt("id")
	usableGroups := h.scenario.ForUser(Input{UserID: userId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": usableGroups})
}

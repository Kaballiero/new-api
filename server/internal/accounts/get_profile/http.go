package get_profile

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) *HTTP { return &HTTP{scenario: scenario} }
func (h *HTTP) Handle(c *gin.Context) {
	id := c.GetInt("id")
	userRole := c.GetInt("role")
	responseData, err := h.scenario.Execute(Input{ID: id, AuthenticatedRole: userRole})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": responseData})
	return
}

package update_channel_balance

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, failure := h.scenario.Execute(Input{ID: id})
	if failure != nil {
		if failure.Kind == "internal" {
			common.ApiError(c, failure.Cause)
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		}
		return
	}
	response := gin.H{"success": true, "message": ""}
	if result.RawResponse == "" {
		response["balance"] = result.Balance
	} else {
		response["raw_response"] = result.RawResponse
	}
	c.JSON(http.StatusOK, response)
}

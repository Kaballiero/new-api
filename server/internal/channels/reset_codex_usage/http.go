package reset_codex_usage

import (
	"fmt"
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
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}
	output, failure := h.scenario.Execute(c.Request.Context(), Input{ChannelID: id})
	if failure != nil {
		if failure.Kind == "internal" {
			common.ApiError(c, failure.Cause)
		} else {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": failure.Message})
		}
		return
	}
	c.JSON(http.StatusOK, output)
}

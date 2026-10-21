package get_channel_operations

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type HTTP struct{ scenario Usecase }

func NewHTTP(scenario Usecase) HTTP { return HTTP{scenario: scenario} }
func (h HTTP) Handle(c *gin.Context) {
	output := h.scenario.Execute()
	common.ApiSuccess(c, gin.H{"retry_times": output.RetryTimes})
}

package get_channel_operations

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

type HTTP struct{ retryTimes func() int }

func NewHTTP(retryTimes func() int) HTTP { return HTTP{retryTimes: retryTimes} }
func (h HTTP) Handle(c *gin.Context) {
	scenario := NewUsecase(h.retryTimes)
	_ = scenario
	output := scenario.Execute()
	common.ApiSuccess(c, gin.H{"retry_times": output.RetryTimes})
}

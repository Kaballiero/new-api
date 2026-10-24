package taskruntime

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
)

func ProjectClientTaskData(data []byte, model string) ([]byte, error) {
	c := &gin.Context{}
	common.BindClientResponseModel(c, model)
	return common.ProjectClientResponse(c, data)
}

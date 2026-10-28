package taskruntime

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

func ProjectClientTaskData(data []byte, model string) ([]byte, error) {
	c := &gin.Context{}
	common.BindClientResponseModel(c, model)
	return common.ProjectClientResponse(c, data)
}

package delete_invalid_codes

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/redemption/transport"
)

type HTTP struct {
	usecase Usecase
}

func NewHTTP(usecase Usecase) HTTP {
	return HTTP{usecase: usecase}
}

func (h HTTP) Handle(c *gin.Context) {
	count, err := h.usecase.Execute()
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, count)
}

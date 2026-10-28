package delete_invalid_codes

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/redemption/transport"

	"github.com/gin-gonic/gin"
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

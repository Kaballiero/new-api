package get_code

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/redemption/dto"
	"github.com/QuantumNous/new-api/internal/redemption/transport"
)

type HTTP struct {
	usecase Usecase
}

func NewHTTP(usecase Usecase) HTTP {
	return HTTP{usecase: usecase}
}

func (h HTTP) Handle(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	code, err := h.usecase.Execute(id)
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.FromCode(code))
}

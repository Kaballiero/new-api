package update_code

import (
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
	statusOnly := c.Query("status_only")
	redemption := dto.Redemption{}
	if err := c.ShouldBindJSON(&redemption); err != nil {
		common.ApiError(c, err)
		return
	}
	code, err := h.usecase.Execute(Input{ID: redemption.Id, Name: redemption.Name, Quota: redemption.Quota,
		Status: redemption.Status, ExpiredTime: redemption.ExpiredTime, StatusOnly: statusOnly != ""})
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(c, dto.FromCode(code))
}

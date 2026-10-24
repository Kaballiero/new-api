package get_own_subscriptions

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/subscriptions/dto"
	"github.com/QuantumNous/new-api/internal/subscriptions/transport"
)

type HTTP struct{ usecase Usecase }

func NewHTTP(usecase Usecase) HTTP { return HTTP{usecase: usecase} }
func (h HTTP) Handle(c *gin.Context) {
	result, err := h.usecase.Execute(c.GetInt("id"))
	if err != nil {
		transport.WriteError(c, err)
		return
	}
	common.ApiSuccess(
		c,
		gin.H{
			"billing_preference": result.Preference,
			"subscriptions":      dto.Summaries(result.Active),
			"all_subscriptions":  dto.Summaries(result.All),
		},
	)
}

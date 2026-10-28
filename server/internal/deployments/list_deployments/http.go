package list_deployments

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	settings func() domain.Settings
	scenario Usecase
}

func NewHTTP(settings func() domain.Settings, scenario Usecase) HTTP {
	return HTTP{settings: settings, scenario: scenario}
}
func (h HTTP) Handle(c *gin.Context) {
	page := common.GetPageQuery(c)
	key, ok := transport.Key(c, h.settings)
	if !ok {
		return
	}
	out, err := h.scenario.Execute(Input{APIKey: key, Page: page.GetPage(), PageSize: page.GetPageSize(), Status: c.Query("status")})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, out)
}

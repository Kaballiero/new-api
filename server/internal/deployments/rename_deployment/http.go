package rename_deployment

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
	key, ok := transport.Key(c, h.settings)
	if !ok {
		return
	}
	id, ok := transport.RequireDeploymentID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, common.PreserveJSONTypeError(err, "github.com/QuantumNous/new-api/internal/deployments/", "ionet"))
		return
	}
	out, err := h.scenario.Execute(Input{APIKey: key, ID: id, Name: req.Name})
	if err != nil {
		transport.Error(c, err)
		return
	}
	common.ApiSuccess(c, out)
}

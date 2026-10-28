package terminate_deployment

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"

	"github.com/gin-gonic/gin"
)

type Provider interface {
	DeleteDeployment(id string) (*domain.UpdateDeploymentResponse, error)
}
type HTTP struct {
	settings func() domain.Settings
	provider func(string) Provider
}

func NewHTTP(settings func() domain.Settings, provider func(string) Provider) HTTP {
	return HTTP{settings: settings, provider: provider}
}
func (h HTTP) Handle(c *gin.Context) {
	client, ok := transport.Provider(c, h.settings, h.provider)
	if !ok {
		return
	}

	deploymentID, ok := transport.RequireDeploymentID(c)
	if !ok {
		return
	}

	resp, err := client.DeleteDeployment(deploymentID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"status":        resp.Status,
		"deployment_id": resp.DeploymentID,
		"message":       i18n.T(c, i18n.MsgDeploymentManageTerminated),
	}
	common.ApiSuccess(c, data)
}

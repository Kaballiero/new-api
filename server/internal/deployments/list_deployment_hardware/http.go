package list_deployment_hardware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"

	"github.com/gin-gonic/gin"
)

type Provider interface {
	ListHardwareTypes() ([]HardwareType, int, error)
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

	hardwareTypes, totalAvailable, err := client.ListHardwareTypes()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := gin.H{
		"hardware_types":  hardwareTypes,
		"total":           len(hardwareTypes),
		"total_available": totalAvailable,
	}
	common.ApiSuccess(c, data)
}

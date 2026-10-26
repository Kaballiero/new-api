package list_deployment_locations

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	ListLocations() (*LocationsResponse, error)
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

	locationsResp, err := client.ListLocations()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	total := locationsResp.Total
	if total == 0 {
		total = len(locationsResp.Locations)
	}

	data := gin.H{
		"locations": locationsResp.Locations,
		"total":     total,
	}
	common.ApiSuccess(c, data)
}

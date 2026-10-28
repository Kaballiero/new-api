package list_deployment_containers

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"

	"github.com/gin-gonic/gin"
)

type Provider interface {
	ListContainers(id string) (*domain.ContainerList, error)
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

	containers, err := client.ListContainers(deploymentID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]map[string]any, 0)
	if containers != nil {
		items = make([]map[string]any, 0, len(containers.Workers))
		for _, ctr := range containers.Workers {
			events := make([]map[string]any, 0, len(ctr.ContainerEvents))
			for _, event := range ctr.ContainerEvents {
				events = append(events, map[string]any{
					"time":    event.Time.Unix(),
					"message": event.Message,
				})
			}

			items = append(items, map[string]any{
				"container_id":       ctr.ContainerID,
				"device_id":          ctr.DeviceID,
				"status":             strings.ToLower(strings.TrimSpace(ctr.Status)),
				"hardware":           ctr.Hardware,
				"brand_name":         ctr.BrandName,
				"created_at":         ctr.CreatedAt.Unix(),
				"uptime_percent":     ctr.UptimePercent,
				"gpus_per_container": ctr.GPUsPerContainer,
				"public_url":         ctr.PublicURL,
				"events":             events,
			})
		}
	}

	response := gin.H{
		"total":      0,
		"containers": items,
	}
	if containers != nil {
		response["total"] = containers.Total
	}

	common.ApiSuccess(c, response)
}

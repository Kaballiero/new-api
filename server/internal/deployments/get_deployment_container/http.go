package get_deployment_container

import (
	"strings"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	GetContainerDetails(id, container string) (*domain.Container, error)
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

	containerID, ok := transport.RequireContainerID(c)
	if !ok {
		return
	}

	details, err := client.GetContainerDetails(deploymentID, containerID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if details == nil {
		common.ApiErrorMsg(c, i18n.T(c, i18n.MsgDeploymentManageContainerNotFound))
		return
	}

	events := make([]map[string]any, 0, len(details.ContainerEvents))
	for _, event := range details.ContainerEvents {
		events = append(events, map[string]any{
			"time":    event.Time.Unix(),
			"message": event.Message,
		})
	}

	data := gin.H{
		"deployment_id":      deploymentID,
		"container_id":       details.ContainerID,
		"device_id":          details.DeviceID,
		"status":             strings.ToLower(strings.TrimSpace(details.Status)),
		"hardware":           details.Hardware,
		"brand_name":         details.BrandName,
		"created_at":         details.CreatedAt.Unix(),
		"uptime_percent":     details.UptimePercent,
		"gpus_per_container": details.GPUsPerContainer,
		"public_url":         details.PublicURL,
		"events":             events,
	}

	common.ApiSuccess(c, data)
}

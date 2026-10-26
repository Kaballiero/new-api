package get_deployment

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/transport"
	"github.com/gin-gonic/gin"
)

type Provider interface {
	GetDeployment(id string) (*domain.DeploymentDetail, error)
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

	details, err := client.GetDeployment(deploymentID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	data := map[string]any{
		"id":              details.ID,
		"deployment_name": details.ID,
		"model_name":      "",
		"model_version":   "",
		"status":          strings.ToLower(details.Status),
		"instance_count":  details.TotalContainers,
		"hardware_id":     details.HardwareID,
		"resource_config": map[string]any{
			"cpu":    "",
			"memory": "",
			"gpu":    strconv.Itoa(details.TotalGPUs),
		},
		"created_at":                details.CreatedAt.Unix(),
		"updated_at":                details.CreatedAt.Unix(),
		"description":               "",
		"amount_paid":               details.AmountPaid,
		"completed_percent":         details.CompletedPercent,
		"gpus_per_container":        details.GPUsPerContainer,
		"total_gpus":                details.TotalGPUs,
		"total_containers":          details.TotalContainers,
		"hardware_name":             details.HardwareName,
		"brand_name":                details.BrandName,
		"compute_minutes_served":    details.ComputeMinutesServed,
		"compute_minutes_remaining": details.ComputeMinutesRemaining,
		"locations":                 details.Locations,
		"container_config":          details.ContainerConfig,
	}

	common.ApiSuccess(c, data)
}

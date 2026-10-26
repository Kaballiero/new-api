package domain

import (
	"time"
)

type Deployment struct {
	ID                      string    `json:"id"`
	Status                  string    `json:"status"`
	Name                    string    `json:"name"`
	CompletedPercent        float64   `json:"completed_percent"`
	HardwareQuantity        int       `json:"hardware_quantity"`
	BrandName               string    `json:"brand_name"`
	HardwareName            string    `json:"hardware_name"`
	ComputeMinutesServed    int       `json:"compute_minutes_served"`
	ComputeMinutesRemaining int       `json:"compute_minutes_remaining"`
	CreatedAt               time.Time `json:"created_at"`
}

type DeploymentDetail struct {
	ID                      string                    `json:"id"`
	Status                  string                    `json:"status"`
	CreatedAt               time.Time                 `json:"created_at"`
	AmountPaid              float64                   `json:"amount_paid"`
	CompletedPercent        float64                   `json:"completed_percent"`
	TotalGPUs               int                       `json:"total_gpus"`
	GPUsPerContainer        int                       `json:"gpus_per_container"`
	TotalContainers         int                       `json:"total_containers"`
	HardwareName            string                    `json:"hardware_name"`
	HardwareID              int                       `json:"hardware_id"`
	Locations               []DeploymentLocation      `json:"locations"`
	BrandName               string                    `json:"brand_name"`
	ComputeMinutesServed    int                       `json:"compute_minutes_served"`
	ComputeMinutesRemaining int                       `json:"compute_minutes_remaining"`
	ContainerConfig         DeploymentContainerConfig `json:"container_config"`
}

type DeploymentLocation struct {
	ID   int    `json:"id"`
	ISO2 string `json:"iso2"`
	Name string `json:"name"`
}

type DeploymentContainerConfig struct {
	Entrypoint   []string       `json:"entrypoint"`
	EnvVariables map[string]any `json:"env_variables"`
	TrafficPort  int            `json:"traffic_port"`
	ImageURL     string         `json:"image_url"`
}

type DeploymentList struct {
	Deployments []Deployment `json:"deployments"`
	Total       int          `json:"total"`
	Statuses    []string     `json:"statuses"`
}

type Container struct {
	DeviceID         string           `json:"device_id"`
	ContainerID      string           `json:"container_id"`
	Hardware         string           `json:"hardware"`
	BrandName        string           `json:"brand_name"`
	CreatedAt        time.Time        `json:"created_at"`
	UptimePercent    int              `json:"uptime_percent"`
	GPUsPerContainer int              `json:"gpus_per_container"`
	Status           string           `json:"status"`
	ContainerEvents  []ContainerEvent `json:"container_events"`
	PublicURL        string           `json:"public_url"`
}

type ContainerEvent struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

type ContainerList struct {
	Total   int         `json:"total"`
	Workers []Container `json:"workers"`
}

type ListDeploymentsOptions struct {
	Status     string `json:"status,omitempty"`      // filter by status
	LocationID int    `json:"location_id,omitempty"` // filter by location
	Page       int    `json:"page,omitempty"`        // pagination
	PageSize   int    `json:"page_size,omitempty"`   // pagination
	SortBy     string `json:"sort_by,omitempty"`     // sort field
	SortOrder  string `json:"sort_order,omitempty"`  // asc/desc
}

type UpdateDeploymentResponse struct {
	Status       string `json:"status"`
	DeploymentID string `json:"deployment_id"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return e.Message + ": " + e.Details
	}
	return e.Message
}

type Settings struct {
	Enabled bool
	APIKey  string
}

package deployments

import (
	"github.com/QuantumNous/new-api/internal/deployments/create_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/domain"
	"github.com/QuantumNous/new-api/internal/deployments/estimate_deployment_price"
	"github.com/QuantumNous/new-api/internal/deployments/extend_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/get_available_replicas"
	"github.com/QuantumNous/new-api/internal/deployments/get_deployment_logs"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployment_hardware"
	"github.com/QuantumNous/new-api/internal/deployments/list_deployment_locations"
	"github.com/QuantumNous/new-api/internal/deployments/rename_deployment"
	"github.com/QuantumNous/new-api/internal/deployments/test_deployment_connection"
	"github.com/QuantumNous/new-api/internal/deployments/update_deployment"
	"github.com/QuantumNous/new-api/pkg/ionet"
)

type Client struct{ client *ionet.Client }

func Enterprise(key string) Client { return Client{client: ionet.NewEnterpriseClient(key)} }
func Public(key string) Client     { return Client{client: ionet.NewClient(key)} }
func (c Client) GetMaxGPUsPerContainer() (*test_deployment_connection.MaxGPUResponse, error) {
	value, err := c.client.GetMaxGPUsPerContainer()
	if err != nil {
		if failure, ok := err.(*ionet.APIError); ok {
			return nil, &domain.APIError{Code: failure.Code, Message: failure.Message, Details: failure.Details}
		}
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKMaxGPUResponse(*value)
	return &out, nil
}
func (c Client) ListDeployments(options *domain.ListDeploymentsOptions) (*domain.DeploymentList, error) {
	wire := toSDKListDeploymentsOptions(*options)
	value, err := c.client.ListDeployments(&wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKDeploymentList(*value)
	return &out, nil
}
func (c Client) GetDeployment(id string) (*domain.DeploymentDetail, error) {
	value, err := c.client.GetDeployment(id)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKDeploymentDetail(*value)
	return &out, nil
}
func (c Client) CheckClusterNameAvailability(name string) (bool, error) {
	return c.client.CheckClusterNameAvailability(name)
}
func (c Client) UpdateClusterName(id string, request *rename_deployment.UpdateClusterNameRequest) (*rename_deployment.UpdateClusterNameResponse, error) {
	wire := toSDKUpdateClusterNameRequest(*request)
	value, err := c.client.UpdateClusterName(id, &wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKUpdateClusterNameResponse(*value)
	return &out, nil
}
func (c Client) UpdateDeployment(id string, request *update_deployment.UpdateDeploymentRequest) (*domain.UpdateDeploymentResponse, error) {
	wire := toSDKUpdateDeploymentRequest(*request)
	value, err := c.client.UpdateDeployment(id, &wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKUpdateDeploymentResponse(*value)
	return &out, nil
}
func (c Client) ExtendDeployment(id string, request *extend_deployment.ExtendDurationRequest) (*domain.DeploymentDetail, error) {
	wire := toSDKExtendDurationRequest(*request)
	value, err := c.client.ExtendDeployment(id, &wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKDeploymentDetail(*value)
	return &out, nil
}
func (c Client) DeleteDeployment(id string) (*domain.UpdateDeploymentResponse, error) {
	value, err := c.client.DeleteDeployment(id)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKUpdateDeploymentResponse(*value)
	return &out, nil
}
func (c Client) DeployContainer(request *create_deployment.DeploymentRequest) (*create_deployment.DeploymentResponse, error) {
	wire := toSDKDeploymentRequest(*request)
	value, err := c.client.DeployContainer(&wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKDeploymentResponse(*value)
	return &out, nil
}
func (c Client) ListLocations() (*list_deployment_locations.LocationsResponse, error) {
	value, err := c.client.ListLocations()
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKLocationsResponse(*value)
	return &out, nil
}
func (c Client) GetAvailableReplicas(hardware, count int) (*get_available_replicas.AvailableReplicasResponse, error) {
	value, err := c.client.GetAvailableReplicas(hardware, count)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKAvailableReplicasResponse(*value)
	return &out, nil
}
func (c Client) GetPriceEstimation(request *estimate_deployment_price.PriceEstimationRequest) (*estimate_deployment_price.PriceEstimationResponse, error) {
	wire := toSDKPriceEstimationRequest(*request)
	value, err := c.client.GetPriceEstimation(&wire)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKPriceEstimationResponse(*value)
	return &out, nil
}
func (c Client) GetContainerLogsRaw(id, container string, options *get_deployment_logs.GetLogsOptions) (string, error) {
	wire := toSDKGetLogsOptions(*options)
	return c.client.GetContainerLogsRaw(id, container, &wire)
}
func (c Client) ListContainers(id string) (*domain.ContainerList, error) {
	value, err := c.client.ListContainers(id)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKContainerList(*value)
	return &out, nil
}
func (c Client) GetContainerDetails(id, container string) (*domain.Container, error) {
	value, err := c.client.GetContainerDetails(id, container)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	out := fromSDKContainer(*value)
	return &out, nil
}
func (c Client) ListHardwareTypes() ([]list_deployment_hardware.HardwareType, int, error) {
	v, n, err := c.client.ListHardwareTypes()
	return mapSlice(v, fromSDKHardwareType), n, err
}
func fromSDKAvailableReplica(value ionet.AvailableReplica) get_available_replicas.AvailableReplica {
	return get_available_replicas.AvailableReplica{
		LocationID:     value.LocationID,
		LocationName:   value.LocationName,
		HardwareID:     value.HardwareID,
		HardwareName:   value.HardwareName,
		AvailableCount: value.AvailableCount,
		MaxGPUs:        value.MaxGPUs,
	}
}
func fromSDKAvailableReplicasResponse(value ionet.AvailableReplicasResponse) get_available_replicas.AvailableReplicasResponse {
	return get_available_replicas.AvailableReplicasResponse{Replicas: mapSlice(value.Replicas, fromSDKAvailableReplica)}
}
func fromSDKContainer(value ionet.Container) domain.Container {
	return domain.Container{
		DeviceID:         value.DeviceID,
		ContainerID:      value.ContainerID,
		Hardware:         value.Hardware,
		BrandName:        value.BrandName,
		CreatedAt:        value.CreatedAt,
		UptimePercent:    value.UptimePercent,
		GPUsPerContainer: value.GPUsPerContainer,
		Status:           value.Status,
		ContainerEvents:  mapSlice(value.ContainerEvents, fromSDKContainerEvent),
		PublicURL:        value.PublicURL,
	}
}
func fromSDKContainerEvent(value ionet.ContainerEvent) domain.ContainerEvent {
	return domain.ContainerEvent{Time: value.Time, Message: value.Message}
}
func fromSDKContainerList(value ionet.ContainerList) domain.ContainerList {
	return domain.ContainerList{Total: value.Total, Workers: mapSlice(value.Workers, fromSDKContainer)}
}
func fromSDKDeployment(value ionet.Deployment) domain.Deployment {
	return domain.Deployment{
		ID:                      value.ID,
		Status:                  value.Status,
		Name:                    value.Name,
		CompletedPercent:        value.CompletedPercent,
		HardwareQuantity:        value.HardwareQuantity,
		BrandName:               value.BrandName,
		HardwareName:            value.HardwareName,
		ComputeMinutesServed:    value.ComputeMinutesServed,
		ComputeMinutesRemaining: value.ComputeMinutesRemaining,
		CreatedAt:               value.CreatedAt,
	}
}
func fromSDKDeploymentContainerConfig(value ionet.DeploymentContainerConfig) domain.DeploymentContainerConfig {
	return domain.DeploymentContainerConfig{
		Entrypoint:   value.Entrypoint,
		EnvVariables: value.EnvVariables,
		TrafficPort:  value.TrafficPort,
		ImageURL:     value.ImageURL,
	}
}
func fromSDKDeploymentDetail(value ionet.DeploymentDetail) domain.DeploymentDetail {
	return domain.DeploymentDetail{
		ID:                      value.ID,
		Status:                  value.Status,
		CreatedAt:               value.CreatedAt,
		AmountPaid:              value.AmountPaid,
		CompletedPercent:        value.CompletedPercent,
		TotalGPUs:               value.TotalGPUs,
		GPUsPerContainer:        value.GPUsPerContainer,
		TotalContainers:         value.TotalContainers,
		HardwareName:            value.HardwareName,
		HardwareID:              value.HardwareID,
		Locations:               mapSlice(value.Locations, fromSDKDeploymentLocation),
		BrandName:               value.BrandName,
		ComputeMinutesServed:    value.ComputeMinutesServed,
		ComputeMinutesRemaining: value.ComputeMinutesRemaining,
		ContainerConfig:         fromSDKDeploymentContainerConfig(value.ContainerConfig),
	}
}
func fromSDKDeploymentList(value ionet.DeploymentList) domain.DeploymentList {
	return domain.DeploymentList{Deployments: mapSlice(value.Deployments, fromSDKDeployment), Total: value.Total, Statuses: value.Statuses}
}
func fromSDKDeploymentLocation(value ionet.DeploymentLocation) domain.DeploymentLocation {
	return domain.DeploymentLocation{ID: value.ID, ISO2: value.ISO2, Name: value.Name}
}
func fromSDKDeploymentResponse(value ionet.DeploymentResponse) create_deployment.DeploymentResponse {
	return create_deployment.DeploymentResponse{DeploymentID: value.DeploymentID, Status: value.Status}
}
func fromSDKHardwareType(value ionet.HardwareType) list_deployment_hardware.HardwareType {
	return list_deployment_hardware.HardwareType{
		ID:             value.ID,
		Name:           value.Name,
		Description:    value.Description,
		GPUType:        value.GPUType,
		GPUMemory:      value.GPUMemory,
		MaxGPUs:        value.MaxGPUs,
		CPU:            value.CPU,
		Memory:         value.Memory,
		Storage:        value.Storage,
		HourlyRate:     value.HourlyRate,
		Available:      value.Available,
		BrandName:      value.BrandName,
		AvailableCount: value.AvailableCount,
	}
}
func fromSDKLocation(value ionet.Location) list_deployment_locations.Location {
	return list_deployment_locations.Location{
		ID:          value.ID,
		Name:        value.Name,
		ISO2:        value.ISO2,
		Region:      value.Region,
		Country:     value.Country,
		Latitude:    value.Latitude,
		Longitude:   value.Longitude,
		Available:   value.Available,
		Description: value.Description,
	}
}
func fromSDKLocationsResponse(value ionet.LocationsResponse) list_deployment_locations.LocationsResponse {
	return list_deployment_locations.LocationsResponse{Locations: mapSlice(value.Locations, fromSDKLocation), Total: value.Total}
}
func fromSDKMaxGPUInfo(value ionet.MaxGPUInfo) test_deployment_connection.MaxGPUInfo {
	return test_deployment_connection.MaxGPUInfo{
		MaxGPUsPerContainer: value.MaxGPUsPerContainer,
		Available:           value.Available,
		HardwareID:          value.HardwareID,
		HardwareName:        value.HardwareName,
		BrandName:           value.BrandName,
	}
}
func fromSDKMaxGPUResponse(value ionet.MaxGPUResponse) test_deployment_connection.MaxGPUResponse {
	return test_deployment_connection.MaxGPUResponse{Hardware: mapSlice(value.Hardware, fromSDKMaxGPUInfo), Total: value.Total}
}
func fromSDKPriceBreakdown(value ionet.PriceBreakdown) estimate_deployment_price.PriceBreakdown {
	return estimate_deployment_price.PriceBreakdown{
		ComputeCost: value.ComputeCost,
		NetworkCost: value.NetworkCost,
		StorageCost: value.StorageCost,
		TotalCost:   value.TotalCost,
		HourlyRate:  value.HourlyRate,
	}
}
func fromSDKPriceEstimationResponse(value ionet.PriceEstimationResponse) estimate_deployment_price.PriceEstimationResponse {
	return estimate_deployment_price.PriceEstimationResponse{
		EstimatedCost:   value.EstimatedCost,
		Currency:        value.Currency,
		PriceBreakdown:  fromSDKPriceBreakdown(value.PriceBreakdown),
		EstimationValid: value.EstimationValid,
	}
}
func fromSDKUpdateClusterNameResponse(value ionet.UpdateClusterNameResponse) rename_deployment.UpdateClusterNameResponse {
	return rename_deployment.UpdateClusterNameResponse{Status: value.Status, Message: value.Message}
}
func fromSDKUpdateDeploymentResponse(value ionet.UpdateDeploymentResponse) domain.UpdateDeploymentResponse {
	return domain.UpdateDeploymentResponse{Status: value.Status, DeploymentID: value.DeploymentID}
}
func toSDKContainerConfig(value create_deployment.ContainerConfig) ionet.ContainerConfig {
	return ionet.ContainerConfig{
		ReplicaCount:       value.ReplicaCount,
		EnvVariables:       value.EnvVariables,
		SecretEnvVariables: value.SecretEnvVariables,
		Entrypoint:         value.Entrypoint,
		TrafficPort:        value.TrafficPort,
		Args:               value.Args,
	}
}
func toSDKDeploymentRequest(value create_deployment.DeploymentRequest) ionet.DeploymentRequest {
	return ionet.DeploymentRequest{
		ResourcePrivateName: value.ResourcePrivateName,
		DurationHours:       value.DurationHours,
		GPUsPerContainer:    value.GPUsPerContainer,
		HardwareID:          value.HardwareID,
		LocationIDs:         value.LocationIDs,
		ContainerConfig:     toSDKContainerConfig(value.ContainerConfig),
		RegistryConfig:      toSDKRegistryConfig(value.RegistryConfig),
	}
}
func toSDKExtendDurationRequest(value extend_deployment.ExtendDurationRequest) ionet.ExtendDurationRequest {
	return ionet.ExtendDurationRequest{DurationHours: value.DurationHours}
}
func toSDKGetLogsOptions(value get_deployment_logs.GetLogsOptions) ionet.GetLogsOptions {
	return ionet.GetLogsOptions{
		StartTime: value.StartTime,
		EndTime:   value.EndTime,
		Level:     value.Level,
		Stream:    value.Stream,
		Limit:     value.Limit,
		Cursor:    value.Cursor,
		Follow:    value.Follow,
	}
}
func toSDKListDeploymentsOptions(value domain.ListDeploymentsOptions) ionet.ListDeploymentsOptions {
	return ionet.ListDeploymentsOptions{
		Status:     value.Status,
		LocationID: value.LocationID,
		Page:       value.Page,
		PageSize:   value.PageSize,
		SortBy:     value.SortBy,
		SortOrder:  value.SortOrder,
	}
}
func toSDKPriceEstimationRequest(value estimate_deployment_price.PriceEstimationRequest) ionet.PriceEstimationRequest {
	return ionet.PriceEstimationRequest{
		LocationIDs:      value.LocationIDs,
		HardwareID:       value.HardwareID,
		GPUsPerContainer: value.GPUsPerContainer,
		DurationHours:    value.DurationHours,
		ReplicaCount:     value.ReplicaCount,
		Currency:         value.Currency,
		DurationType:     value.DurationType,
		DurationQty:      value.DurationQty,
		HardwareQty:      value.HardwareQty,
	}
}
func toSDKRegistryConfig(value create_deployment.RegistryConfig) ionet.RegistryConfig {
	return ionet.RegistryConfig{ImageURL: value.ImageURL, RegistryUsername: value.RegistryUsername, RegistrySecret: value.RegistrySecret}
}
func toSDKUpdateClusterNameRequest(value rename_deployment.UpdateClusterNameRequest) ionet.UpdateClusterNameRequest {
	return ionet.UpdateClusterNameRequest{Name: value.Name}
}
func toSDKUpdateDeploymentRequest(value update_deployment.UpdateDeploymentRequest) ionet.UpdateDeploymentRequest {
	return ionet.UpdateDeploymentRequest{
		EnvVariables:       value.EnvVariables,
		SecretEnvVariables: value.SecretEnvVariables,
		Entrypoint:         value.Entrypoint,
		TrafficPort:        value.TrafficPort,
		ImageURL:           value.ImageURL,
		RegistryUsername:   value.RegistryUsername,
		RegistrySecret:     value.RegistrySecret,
		Args:               value.Args,
		Command:            value.Command,
	}
}
func mapSlice[A, B any](values []A, convert func(A) B) []B {
	if values == nil {
		return nil
	}
	out := make([]B, len(values))
	for i := range values {
		out[i] = convert(values[i])
	}
	return out
}

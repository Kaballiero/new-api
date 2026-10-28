package estimate_deployment_price

type PriceEstimationRequest struct {
	LocationIDs      []int  `json:"location_ids"`
	HardwareID       int    `json:"hardware_id"`
	GPUsPerContainer int    `json:"gpus_per_container"`
	DurationHours    int    `json:"duration_hours"`
	ReplicaCount     int    `json:"replica_count"`
	Currency         string `json:"currency"`
	DurationType     string `json:"duration_type"`
	DurationQty      int    `json:"duration_qty"`
	HardwareQty      int    `json:"hardware_qty"`
}

type PriceEstimationResponse struct {
	EstimatedCost   float64        `json:"estimated_cost"`
	Currency        string         `json:"currency"`
	PriceBreakdown  PriceBreakdown `json:"price_breakdown"`
	EstimationValid bool           `json:"estimation_valid"`
}

type PriceBreakdown struct {
	ComputeCost float64 `json:"compute_cost"`
	NetworkCost float64 `json:"network_cost,omitempty"`
	StorageCost float64 `json:"storage_cost,omitempty"`
	TotalCost   float64 `json:"total_cost"`
	HourlyRate  float64 `json:"hourly_rate"`
}

package get_available_replicas

type AvailableReplica struct {
	LocationID     int    `json:"location_id"`
	LocationName   string `json:"location_name"`
	HardwareID     int    `json:"hardware_id"`
	HardwareName   string `json:"hardware_name"`
	AvailableCount int    `json:"available_count"`
	MaxGPUs        int    `json:"max_gpus"`
}

type AvailableReplicasResponse struct {
	Replicas []AvailableReplica `json:"replicas"`
}

package test_deployment_connection

type MaxGPUResponse struct {
	Hardware []MaxGPUInfo `json:"hardware"`
	Total    int          `json:"total"`
}

type MaxGPUInfo struct {
	MaxGPUsPerContainer int    `json:"max_gpus_per_container"`
	Available           int    `json:"available"`
	HardwareID          int    `json:"hardware_id"`
	HardwareName        string `json:"hardware_name"`
	BrandName           string `json:"brand_name"`
}

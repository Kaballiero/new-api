package list_deployment_hardware

type HardwareType struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description,omitempty"`
	GPUType        string  `json:"gpu_type"`
	GPUMemory      int     `json:"gpu_memory"` // in GB
	MaxGPUs        int     `json:"max_gpus"`
	CPU            string  `json:"cpu,omitempty"`
	Memory         int     `json:"memory,omitempty"`  // in GB
	Storage        int     `json:"storage,omitempty"` // in GB
	HourlyRate     float64 `json:"hourly_rate"`
	Available      bool    `json:"available"`
	BrandName      string  `json:"brand_name,omitempty"`
	AvailableCount int     `json:"available_count,omitempty"`
}

package create_deployment

type DeploymentRequest struct {
	ResourcePrivateName string          `json:"resource_private_name"`
	DurationHours       int             `json:"duration_hours"`
	GPUsPerContainer    int             `json:"gpus_per_container"`
	HardwareID          int             `json:"hardware_id"`
	LocationIDs         []int           `json:"location_ids"`
	ContainerConfig     ContainerConfig `json:"container_config"`
	RegistryConfig      RegistryConfig  `json:"registry_config"`
}

type ContainerConfig struct {
	ReplicaCount       int               `json:"replica_count"`
	EnvVariables       map[string]string `json:"env_variables,omitempty"`
	SecretEnvVariables map[string]string `json:"secret_env_variables,omitempty"`
	Entrypoint         []string          `json:"entrypoint,omitempty"`
	TrafficPort        int               `json:"traffic_port,omitempty"`
	Args               []string          `json:"args,omitempty"`
}

type RegistryConfig struct {
	ImageURL         string `json:"image_url"`
	RegistryUsername string `json:"registry_username,omitempty"`
	RegistrySecret   string `json:"registry_secret,omitempty"`
}

type DeploymentResponse struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
}

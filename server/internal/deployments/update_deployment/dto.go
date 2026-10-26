package update_deployment

type UpdateDeploymentRequest struct {
	EnvVariables       map[string]string `json:"env_variables,omitempty"`
	SecretEnvVariables map[string]string `json:"secret_env_variables,omitempty"`
	Entrypoint         []string          `json:"entrypoint,omitempty"`
	TrafficPort        *int              `json:"traffic_port,omitempty"`
	ImageURL           string            `json:"image_url,omitempty"`
	RegistryUsername   string            `json:"registry_username,omitempty"`
	RegistrySecret     string            `json:"registry_secret,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Command            string            `json:"command,omitempty"`
}

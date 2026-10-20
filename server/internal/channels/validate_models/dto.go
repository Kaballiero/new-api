package validate_models

// ValidateModelsRequest is the body of POST /api/channel/validate_models.
// Either ChannelId>0 (validate a saved channel) OR Type/Key/BaseURL (validate a
// not-yet-saved channel, mirroring FetchModels). When Models is empty and a
// ChannelId is given, the channel's full model list is validated.
type ValidateModelsRequest struct {
	ChannelId    int      `json:"channel_id"`
	Type         int      `json:"type"`
	Key          string   `json:"key"`
	BaseURL      string   `json:"base_url"`
	Models       []string `json:"models"`
	EndpointType string   `json:"endpoint_type"`
}

type ModelValidationResult struct {
	Model        string `json:"model"`
	OK           bool   `json:"ok"`
	Status       string `json:"status"` // alive | dead | uncertain
	UpstreamCode int    `json:"upstream_code"`
	ErrorCode    string `json:"error_code"`
	Message      string `json:"message"`
	LatencyMs    int64  `json:"latency_ms"`
}

type Summary struct {
	Alive     int `json:"alive"`
	Dead      int `json:"dead"`
	Uncertain int `json:"uncertain"`
}

type ValidateModelsResponse struct {
	Results []ModelValidationResult `json:"results"`
	Summary Summary                 `json:"summary"`
}

package domain

type Model struct {
	Id                 int      `json:"id"`
	ModelName          string   `json:"model_name"`
	Description        string   `json:"description,omitempty"`
	Icon               string   `json:"icon,omitempty"`
	Tags               string   `json:"tags,omitempty"`
	VendorID           int      `json:"vendor_id,omitempty"`
	Endpoints          string   `json:"endpoints,omitempty"`
	SupportedEndpoints []string `json:"supported_endpoints,omitempty"`
	Status             int      `json:"status"`
	SyncOfficial       int      `json:"sync_official"`
	CreatedTime        int64    `json:"created_time"`
	UpdatedTime        int64    `json:"updated_time"`

	BoundChannels []BoundChannel `json:"bound_channels,omitempty"`
	EnableGroups  []string       `json:"enable_groups,omitempty"`
	QuotaTypes    []int          `json:"quota_types,omitempty"`
	NameRule      int            `json:"name_rule"`

	MatchedModels []string `json:"matched_models,omitempty"`
	MatchedCount  int      `json:"matched_count,omitempty"`

	HasMetadata            bool             `json:"has_metadata"`
	ConfiguredChannelCount int              `json:"configured_channel_count"`
	SquareState            ModelSquareState `json:"square_state"`
}

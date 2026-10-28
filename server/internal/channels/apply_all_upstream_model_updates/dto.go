package apply_all_upstream_model_updates

type Result struct {
	ChannelID             int      `json:"channel_id"`
	ChannelName           string   `json:"channel_name"`
	AddedModels           []string `json:"added_models"`
	RemovedModels         []string `json:"removed_models"`
	RemainingModels       []string `json:"remaining_models"`
	RemainingRemoveModels []string `json:"remaining_remove_models"`
}

type Output struct {
	ProcessedChannels int      `json:"processed_channels"`
	AddedModels       int      `json:"added_models"`
	RemovedModels     int      `json:"removed_models"`
	FailedChannelIDs  []int    `json:"failed_channel_ids"`
	Results           []Result `json:"results"`
}

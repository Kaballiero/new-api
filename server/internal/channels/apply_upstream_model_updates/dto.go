package apply_upstream_model_updates

type Input struct {
	ID           int      `json:"id"`
	AddModels    []string `json:"add_models"`
	RemoveModels []string `json:"remove_models"`
	IgnoreModels []string `json:"ignore_models"`
}

type Output struct {
	ID                    int      `json:"id"`
	AddedModels           []string `json:"added_models"`
	RemovedModels         []string `json:"removed_models"`
	IgnoredModels         []string `json:"ignored_models"`
	RemainingModels       []string `json:"remaining_models"`
	RemainingRemoveModels []string `json:"remaining_remove_models"`
	Models                string   `json:"models"`
	Settings              string   `json:"settings"`
}

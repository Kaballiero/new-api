package domain

type ModelUpdateChannelSummary struct {
	ChannelName string
	AddCount    int
	RemoveCount int
}

// ModelUpdateReport is the content of the upstream model update notification
// sent after a scan that found changes or failures.
type ModelUpdateReport struct {
	CheckedChannels      int
	ChangedChannels      int
	DetectedAddModels    int
	DetectedRemoveModels int
	AutoAddedModels      int
	FailedChannelIDs     []int
	Channels             []ModelUpdateChannelSummary
	AddModelSamples      []string
	RemoveModelSamples   []string
}

type ModelUpdateSummary struct {
	CheckedChannels      int `json:"checked_channels"`
	ChangedChannels      int `json:"changed_channels"`
	DetectedAddModels    int `json:"detected_add_models"`
	DetectedRemoveModels int `json:"detected_remove_models"`
	FailedChannels       int `json:"failed_channels"`
	AutoAddedModels      int `json:"auto_added_models"`
}

func RepeatedModelUpdateNotification(now int64, changed, failed int, last int64, lastChanged, lastFailed int, window int64) bool {
	return last > 0 && now-last < window && lastChanged == changed && lastFailed == failed
}

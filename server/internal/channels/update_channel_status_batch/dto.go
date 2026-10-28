package update_channel_status_batch

type Input struct {
	Ids    []int `json:"ids"`
	Status int   `json:"status"`
}

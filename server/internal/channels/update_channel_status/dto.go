package update_channel_status

type Input struct {
	ID     int `json:"-"`
	Status int `json:"status"`
}

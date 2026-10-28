package reveal_channel_key

type Input struct{ ID int }
type ChannelKeyResponse struct {
	Key string `json:"key"`
}

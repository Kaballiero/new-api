package delete_channels_batch

type Input struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

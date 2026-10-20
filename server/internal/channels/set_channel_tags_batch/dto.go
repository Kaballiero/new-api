package set_channel_tags_batch

type Input struct {
	Ids []int   `json:"ids"`
	Tag *string `json:"tag"`
}

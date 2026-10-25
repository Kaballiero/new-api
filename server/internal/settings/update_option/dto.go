package update_option

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

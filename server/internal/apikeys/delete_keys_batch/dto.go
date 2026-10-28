package delete_keys_batch

type Request struct {
	Ids []int `json:"ids"`
}
type Output struct {
	Count int
	Keys  map[int]string
}

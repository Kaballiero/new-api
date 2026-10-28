package clear_affinity_cache

type Input struct{ All, Rule string }
type Output struct {
	Deleted int `json:"deleted"`
}

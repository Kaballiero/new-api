package copy_channel

type Input struct {
	ID           int
	Suffix       string
	ResetBalance bool
}
type Output struct {
	ID int `json:"id"`
}

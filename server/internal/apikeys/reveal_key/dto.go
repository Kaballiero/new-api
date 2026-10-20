package reveal_key

type Input struct{ ID, UserID int }
type TokenKeyResponse struct {
	Key string `json:"key"`
}

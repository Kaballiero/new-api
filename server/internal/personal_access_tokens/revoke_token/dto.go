package revoke_token

type Input struct{ UserID int }
type Output struct{ TokenRef string }
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

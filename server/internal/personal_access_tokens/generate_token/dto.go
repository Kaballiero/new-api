package generate_token

type Input struct{ UserID int }
type Output struct{ Token string }
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

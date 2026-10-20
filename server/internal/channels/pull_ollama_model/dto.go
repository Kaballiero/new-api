package pull_ollama_model

type Input struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
}
type Output struct{ Message string }

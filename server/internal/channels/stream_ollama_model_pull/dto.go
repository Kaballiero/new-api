package stream_ollama_model_pull

type Input struct {
	ChannelID int    `json:"channel_id"`
	ModelName string `json:"model_name"`
}

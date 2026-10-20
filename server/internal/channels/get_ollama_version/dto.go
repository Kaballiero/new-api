package get_ollama_version

type Input struct{ ID int }
type Output struct {
	Version string `json:"version"`
}

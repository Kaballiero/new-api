package channels

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
)

type Ollama struct{}

func NewOllama() Ollama                                 { return Ollama{} }
func (Ollama) Pull(base, key, name string) error        { return ollama.PullOllamaModel(base, key, name) }
func (Ollama) Delete(base, key, name string) error      { return ollama.DeleteOllamaModel(base, key, name) }
func (Ollama) Version(base, key string) (string, error) { return ollama.FetchOllamaVersion(base, key) }
func (Ollama) Stream(base, key, name string, progress func(domain.PullProgress)) error {
	return ollama.PullOllamaModelStream(base, key, name, func(p ollama.OllamaPullResponse) { progress(domain.PullProgress(p)) })
}

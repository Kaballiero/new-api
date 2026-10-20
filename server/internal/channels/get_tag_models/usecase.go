package get_tag_models

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
)

type Store interface {
	ByTag(string) ([]*domain.Channel, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute(input Input) (Output, error) {
	channels, err := usecase.store.ByTag(input.Tag)
	if err != nil {
		return "", err
	}
	longest := ""
	maxLength := 0
	for _, channel := range channels {
		if channel.Models != "" {
			models := strings.Split(channel.Models, ",")
			if len(models) > maxLength {
				maxLength = len(models)
				longest = channel.Models
			}
		}
	}
	return longest, nil
}

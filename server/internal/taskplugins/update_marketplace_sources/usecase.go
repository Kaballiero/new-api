package update_marketplace_sources

import (
	"context"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Settings interface {
	SaveSources([]domain.MarketplaceSource) error
}
type Usecase struct{ settings Settings }

func NewUsecase(settings Settings) Usecase { return Usecase{settings: settings} }

type Input struct{ Sources []domain.MarketplaceSource }

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {

	if input.Sources == nil {
		input.Sources = []domain.MarketplaceSource{}
	}
	for i := range input.Sources {
		name := strings.TrimSpace(input.Sources[i].Name)
		indexURL := strings.TrimSpace(input.Sources[i].IndexURL)
		if name == "" {
			return nil, domain.ErrMarketplaceName
		}
		parsed, err := url.Parse(indexURL)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
			return nil, domain.ErrMarketplaceURL
		}
		input.Sources[i].Name = name
		input.Sources[i].IndexURL = indexURL
	}
	if err := u.settings.SaveSources(input.Sources); err != nil {
		return nil, err
	}
	return input.Sources, nil
}

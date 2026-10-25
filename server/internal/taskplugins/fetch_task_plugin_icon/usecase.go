package fetch_task_plugin_icon

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
}
type Catalog interface{ FactoryIconURI(string) string }
type Decoder interface {
	DecodeIcon(string) (string, []byte, error)
}
type Usecase struct {
	store   Store
	catalog Catalog
	decoder Decoder
}

func NewUsecase(s Store, c Catalog, d Decoder) Usecase {
	return Usecase{store: s, catalog: c, decoder: d}
}

type Input struct{ Key, Version string }

func (u Usecase) Execute(ctx context.Context, input Input) (domain.Icon, error) {
	icon := ""
	p, e := u.store.GetTaskPluginVersion(input.Key, input.Version)
	if e == nil {
		icon = p.Icon
	} else if !errors.Is(e, domain.ErrNotFound) {
		return domain.Icon{}, e
	}
	if icon == "" && input.Version == "" {
		icon = u.catalog.FactoryIconURI(input.Key)
	}
	if icon == "" {
		return domain.Icon{}, domain.ErrNotFound
	}
	media, data, e := u.decoder.DecodeIcon(icon)
	if e != nil {
		return domain.Icon{}, domain.ErrNotFound
	}
	return domain.Icon{MediaType: media, Data: data}, nil
}

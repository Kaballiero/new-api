package dryrun_task_plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
}
type Runtime interface {
	Register(string, domain.Options) (domain.Compiled, error)
	Call(domain.Compiled, string, string, []any) (any, error)
}
type Catalog interface{ FactorySource(string) (string, error) }
type Decoder interface {
	Argument(json.RawMessage, *any) error
}
type Usecase struct {
	store   Store
	runtime Runtime
	catalog Catalog
	decoder Decoder
}

func NewUsecase(s Store, r Runtime, c Catalog, d Decoder) Usecase {
	return Usecase{store: s, runtime: r, catalog: c, decoder: d}
}

type Input struct {
	Key, Hook, Member string
	Args              []json.RawMessage
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {
	source := ""
	p, e := u.store.GetTaskPluginVersion(input.Key, "")
	if e == nil {
		source = p.Source
	} else if errors.Is(e, domain.ErrNotFound) {
		source, e = u.catalog.FactorySource(input.Key)
	}
	if e != nil {
		return nil, domain.ErrPluginNotFound
	}
	loaded, e := u.runtime.Register(source, domain.Options{Key: input.Key})
	if e != nil {
		return nil, e
	}
	args := make([]any, len(input.Args))
	for index, raw := range input.Args {
		if e = u.decoder.Argument(raw, &args[index]); e != nil {
			return nil, fmt.Errorf("invalid argument %d: %v", index+1, e)
		}
	}
	return u.runtime.Call(loaded, input.Hook, input.Member, args)
}

package payments

import (
	"github.com/QuantumNous/new-api/model"
)

type Options struct{}

func (Options) Set(key, value string) error { return model.UpdateOption(key, value) }

func (Options) Bulk(values map[string]string) error { return model.UpdateOptionsBulk(values) }

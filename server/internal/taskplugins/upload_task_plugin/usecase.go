package upload_task_plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface{ SaveTaskPlugin(*domain.Plugin) error }
type Runtime interface {
	DecodeIcon(string) (string, []byte, error)
	Preflight(domain.Compiled) error
	Register(string, domain.Options) (domain.Compiled, error)
	Validate(domain.Compiled) error
}
type Syncer interface{ Execute(context.Context) error }
type Usecase struct {
	store   Store
	runtime Runtime
	syncer  Syncer
}

func NewUsecase(store Store, runtime Runtime, syncer Syncer) Usecase {
	return Usecase{store: store, runtime: runtime, syncer: syncer}
}

type Input struct {
	Source       string
	Enabled      *bool
	Remark       string
	Force        bool
	SourceSha256 string
	// Icon carries the sidecar icon.svg / icon.png as a data URI. It is optional
	// and stored separately from the source so the JavaScript stays readable.
	Icon string
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {

	if len(input.Source) > 1024*1024 {
		return nil, domain.ErrSourceTooLarge
	}
	if expected := strings.TrimSpace(input.SourceSha256); expected != "" {
		actual := fmt.Sprintf("%x", sha256.Sum256([]byte(input.Source)))
		if !strings.EqualFold(actual, expected) {
			return nil, domain.ErrSourceHash
		}
	}
	loaded, err := u.runtime.Register(input.Source, domain.Options{})
	if err != nil {
		return nil, err
	}
	if err = u.runtime.Validate(loaded); err != nil {
		return nil, err
	}
	icon := strings.TrimSpace(input.Icon)
	if icon != "" {
		if _, _, err = u.runtime.DecodeIcon(icon); err != nil {
			return nil, err
		}
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if enabled && !input.Force {
		if err = u.runtime.Preflight(loaded); err != nil {
			return nil, err
		}
	}
	plugin := domain.Plugin{
		Key: loaded.Metadata().Key, APIVersion: loaded.Metadata().APIVersion, Version: loaded.Metadata().Version,
		Source: input.Source, SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(input.Source))),
		Icon: icon, Enabled: enabled, Remark: input.Remark,
	}
	if err = u.store.SaveTaskPlugin(&plugin); err != nil {
		return nil, err
	}
	if err = u.syncer.Execute(ctx); err != nil {
		return nil, err
	}
	return domain.Detail{Plugin: &plugin, Meta: loaded.Metadata(), Source: plugin.Source, Layer: "override", HasIcon: plugin.Icon != ""}, nil
}

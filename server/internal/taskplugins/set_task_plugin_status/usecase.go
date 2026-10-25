package set_task_plugin_status

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
)

type Store interface {
	GetTaskPluginUsage(string) ([]domain.ChannelRef, int64, error)
	GetTaskPluginVersion(string, string) (*domain.Plugin, error)
	SetTaskPluginEnabled(string, bool) error
}
type Runtime interface {
	Snapshot() domain.RegistrySnapshot
}
type Settings interface {
	DisableChannel(int) bool
	DisabledKeys() []string
	SaveDisabledKeys() error
	SetDisabledKeys([]string) error
}
type Syncer interface{ Execute(context.Context) error }
type Usecase struct {
	store    Store
	runtime  Runtime
	settings Settings
	syncer   Syncer
}

func NewUsecase(store Store, runtime Runtime, settings Settings, syncer Syncer) Usecase {
	return Usecase{store: store, runtime: runtime, settings: settings, syncer: syncer}
}

type Input struct {
	Enabled *bool
	Key     string

	Force, Cascade bool
}

func (u Usecase) Execute(ctx context.Context, input Input) (any, error) {

	key := input.Key
	disabledChannels := 0
	if !*input.Enabled {
		channels, inFlight, usageErr := u.store.GetTaskPluginUsage(key)
		if usageErr != nil {
			return nil, usageErr
		}
		cascade := input.Cascade
		force := input.Force
		if (len(channels) > 0 && !cascade) || (inFlight > 0 && !force) {
			return nil, &domain.InUseError{Channels: channels, InFlight: inFlight}
		}
		if cascade {
			for _, channel := range channels {
				if u.settings.DisableChannel(channel.Id) {
					disabledChannels++
				}
			}
		}
	}
	_, lookupErr := u.store.GetTaskPluginVersion(key, "")
	hasActiveOverride := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, domain.ErrNotFound) {
		return nil, lookupErr
	}
	// Switching a key off must silence every layer that can serve it. The
	// factory built-in goes into the disabled set even when an override row
	// exists; otherwise the built-in would keep routing the same models (and
	// blocking same-model uploads) right after the administrator disabled the
	// plugin. Switching on reverses both layers.
	if domain.HasFactory(u.runtime.Snapshot(), key) {
		keys := u.settings.DisabledKeys()
		if *input.Enabled {
			next := make([]string, 0, len(keys))
			for _, item := range keys {
				if item != key {
					next = append(next, item)
				}
			}
			keys = next
		} else {
			keys = append(append([]string{}, keys...), key)
		}
		if err := u.settings.SetDisabledKeys(keys); err != nil {
			return nil, err
		}
		if err := u.settings.SaveDisabledKeys(); err != nil {
			return nil, err
		}

		if !hasActiveOverride {
			return map[string]any{"plugin_enabled": *input.Enabled, "disabled_channels": disabledChannels}, nil
		}
	}
	if err := u.store.SetTaskPluginEnabled(key, *input.Enabled); err != nil {
		return nil, err
	}
	if err := u.syncer.Execute(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"plugin_enabled": *input.Enabled, "disabled_channels": disabledChannels}, nil
}

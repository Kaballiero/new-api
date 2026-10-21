package copy_channel

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Get(int, bool) (*domain.Channel, error)
	ValidateSettings(*domain.Channel) error
	Insert(*domain.Channel) error
}
type Usecase struct {
	store         Store
	canBindPlugin func() bool
	now           func() int64
	log           func(string)
	refresh       func()
	audit         func(map[string]any)
}

func NewUsecase(store Store, canBindPlugin func() bool, now func() int64, log func(string), refresh func(), audit func(map[string]any)) Usecase {
	return Usecase{store: store, canBindPlugin: canBindPlugin, now: now, log: log, refresh: refresh, audit: audit}
}
func (usecase Usecase) Execute(input Input) (Output, error) {
	// fetch original channel with key
	origin, err := usecase.store.Get(input.ID, true)
	if err != nil {
		usecase.log("failed to get channel by id: " + err.Error())
		return Output{}, domain.ErrCopySourceUnavailable
	}
	if origin.Type == constant.ChannelTypeTaskPlugin &&
		!usecase.canBindPlugin() {
		return Output{}, domain.ErrTaskPluginBindDenied
	}
	// clone channel
	clone := *origin // shallow copy is sufficient as we will overwrite primitives
	clone.Id = 0     // let DB auto-generate
	clone.CreatedTime = usecase.now()
	clone.Name = origin.Name + input.Suffix
	clone.TestTime = 0
	clone.ResponseTime = 0
	if input.ResetBalance {
		clone.Balance = 0
		clone.UsedQuota = 0
	}
	if err := usecase.store.ValidateSettings(&clone); err != nil {
		usecase.log("failed to validate cloned channel: " + err.Error())
		return Output{}, domain.ErrCopySettingsInvalid
	}
	// insert
	if err := usecase.store.Insert(&clone); err != nil {
		usecase.log("failed to clone channel: " + err.Error())
		return Output{}, domain.ErrCopyFailed
	}
	usecase.refresh()
	usecase.audit(map[string]any{
		"sourceId": input.ID,
		"id":       clone.Id,
		"name":     clone.Name,
	})
	// success
	return Output{ID: clone.Id}, nil
}

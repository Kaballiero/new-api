package edit_tag_channels

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	EditTag(string, *string, *string, *string, *string, *int64, *uint, *string, *string) error
}
type Usecase struct {
	store             Store
	canWriteSensitive func() bool
	refresh           func()
	audit             func(string)
}

func NewUsecase(store Store, canWriteSensitive func() bool, refresh func(), audit func(string)) Usecase {
	return Usecase{store: store, canWriteSensitive: canWriteSensitive, refresh: refresh, audit: audit}
}

// Execute returns storage failures as they are: they are answered with their
// text and HTTP 200.
func (usecase Usecase) Execute(channelTag ChannelTag) error {
	if channelTag.Tag == "" {
		return domain.ErrTagRequired
	}
	if (channelTag.ParamOverride != nil || channelTag.HeaderOverride != nil) &&
		!usecase.canWriteSensitive() {
		return domain.ErrSensitiveWriteDenied
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			return domain.ErrParamOverrideInvalid
		}
		channelTag.ParamOverride = &trimmed
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			return domain.ErrHeaderOverrideInvalid
		}
		channelTag.HeaderOverride = &trimmed
	}
	err := usecase.store.EditTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Groups, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		return err
	}
	usecase.refresh()
	usecase.audit(channelTag.Tag)
	return nil
}

package edit_tag_channels

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"strings"
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
func (usecase Usecase) Execute(channelTag ChannelTag) *domain.Failure {
	if channelTag.Tag == "" {
		return &domain.Failure{Kind: "invalid", Message: "tag不能为空"}
	}
	if (channelTag.ParamOverride != nil || channelTag.HeaderOverride != nil) &&
		!usecase.canWriteSensitive() {
		return &domain.Failure{Kind: "permission"}
	}
	if channelTag.ParamOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.ParamOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			return &domain.Failure{Kind: "invalid", Message: "参数覆盖必须是合法的 JSON 格式"}
		}
		channelTag.ParamOverride = common.GetPointer[string](trimmed)
	}
	if channelTag.HeaderOverride != nil {
		trimmed := strings.TrimSpace(*channelTag.HeaderOverride)
		if trimmed != "" && !json.Valid([]byte(trimmed)) {
			return &domain.Failure{Kind: "invalid", Message: "请求头覆盖必须是合法的 JSON 格式"}
		}
		channelTag.HeaderOverride = common.GetPointer[string](trimmed)
	}
	err := usecase.store.EditTag(channelTag.Tag, channelTag.NewTag, channelTag.ModelMapping, channelTag.Models, channelTag.Groups, channelTag.Priority, channelTag.Weight, channelTag.ParamOverride, channelTag.HeaderOverride)
	if err != nil {
		return &domain.Failure{Kind: "internal", Cause: err}
	}
	usecase.refresh()
	usecase.audit(channelTag.Tag)
	return nil

}

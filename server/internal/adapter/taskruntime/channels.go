package taskruntime

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
	"github.com/QuantumNous/new-api/model"
)

type PollChannels struct{}

func (PollChannels) Get(channelID int) (poll_tasks.Channel, error) {
	ch, err := model.CacheGetChannel(channelID)
	if err != nil {
		return poll_tasks.Channel{}, err
	}
	return poll_tasks.Channel{Id: ch.Id, BaseURL: ch.GetBaseURL(), FallbackBaseURL: constant.GetChannelBaseURL(ch.Type),
		Key: ch.Key, Proxy: ch.GetSetting().Proxy, DisablePollingSleep: ch.GetOtherSettings().DisableTaskPollingSleep}, nil
}

type MidjourneyChannels struct{}

func (MidjourneyChannels) Get(channelID int) (poll_midjourney_tasks.Channel, error) {
	ch, err := model.CacheGetChannel(channelID)
	if err != nil {
		return poll_midjourney_tasks.Channel{}, err
	}
	return poll_midjourney_tasks.Channel{BaseURL: *ch.BaseURL, Key: ch.Key}, nil
}

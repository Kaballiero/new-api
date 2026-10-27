package taskruntime

import (
	"github.com/QuantumNous/new-api/constant"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/tasks/poll_midjourney_tasks"
	"github.com/QuantumNous/new-api/internal/tasks/poll_tasks"
)

type PollChannels struct{}

func (PollChannels) Get(channelID int) (poll_tasks.Channel, error) {
	ch, err := channelstore.CacheGetChannel(channelID)
	if err != nil {
		return poll_tasks.Channel{}, err
	}
	return poll_tasks.Channel{Id: ch.Id, BaseURL: ch.GetBaseURL(), FallbackBaseURL: constant.GetChannelBaseURL(ch.Type),
		Key: ch.Key, Proxy: channelstore.NewChannelPersistence().GetSetting(ch).Proxy, DisablePollingSleep: channelstore.NewChannelPersistence().GetOtherSettings(ch).DisableTaskPollingSleep}, nil
}

type MidjourneyChannels struct{}

func (MidjourneyChannels) Get(channelID int) (poll_midjourney_tasks.Channel, error) {
	ch, err := channelstore.CacheGetChannel(channelID)
	if err != nil {
		return poll_midjourney_tasks.Channel{}, err
	}
	return poll_midjourney_tasks.Channel{BaseURL: *ch.BaseURL, Key: ch.Key}, nil
}

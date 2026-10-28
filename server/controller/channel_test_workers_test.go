package controller

import (
	"context"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/app"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/channels/run_channel_tests"
	"github.com/QuantumNous/new-api/model"
)

// channelTestSummary records the outcome of one channel test cycle so the
// system task can persist a per-run result for history.
type channelTestSummary = channeldomain.HealthSummary

// runChannelTestWorkers executes independent channel tests with bounded
// concurrency. Results and progress are reduced by the caller goroutine, so
// summary counts and the progress reporter remain serialized.
func runChannelTestWorkers(ctx context.Context, channels []*model.Channel, concurrency int, run func(context.Context, *model.Channel) channelTestSummary, report func(int, int)) channelTestSummary {
	store := channelstore.NewStore()
	values := make([]*channeldomain.Channel, len(channels))
	for i, ch := range channels {
		values[i] = store.Domain(ch)
	}
	return app.NewChannelTestTaskUsecase(channelEffects()).Workers(ctx, values, concurrency, func(ctx context.Context, ch *channeldomain.Channel) channelTestSummary {
		return run(ctx, store.Record(ch))
	}, report)
}

func selectChannelsForAutomaticTest(channels []*model.Channel, mode string) []*model.Channel {
	store := channelstore.NewStore()
	values := make([]*channeldomain.Channel, len(channels))
	for i, ch := range channels {
		values[i] = store.Domain(ch)
	}
	selected := run_channel_tests.SelectChannels(values, mode)
	result := make([]*model.Channel, len(selected))
	for i, ch := range selected {
		result[i] = store.Record(ch)
	}
	return result
}

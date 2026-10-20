package run_channel_tests

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/relaykit/types"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Store interface {
	All() ([]*domain.Channel, error)
	SaveResponseTime(*domain.Channel, int64)
}
type Usecase struct {
	store                Store
	probe                domain.Probe
	user                 func() (int, error)
	stream               func(*domain.Channel) bool
	shouldDisable        func(*types.NewAPIError) bool
	shouldEnable         func(*types.NewAPIError, int) bool
	disableEnabled       func() bool
	enable               func(int, string, string)
	threshold            func() float64
	normalizeConcurrency func(int) int
	interval             func() time.Duration
	mode                 func() string
	concurrency          func() int
	notify               func()
}

func NewUsecase(store Store, probe domain.Probe, user func() (int, error), stream func(*domain.Channel) bool, shouldDisable func(*types.NewAPIError) bool, shouldEnable func(*types.NewAPIError, int) bool, disableEnabled func() bool, enable func(int, string, string), threshold func() float64, normalizeConcurrency func(int) int, interval func() time.Duration, mode func() string, concurrency func() int, notify func()) Usecase {
	return Usecase{store: store, probe: probe, user: user, stream: stream, shouldDisable: shouldDisable, shouldEnable: shouldEnable, disableEnabled: disableEnabled, enable: enable, threshold: threshold, normalizeConcurrency: normalizeConcurrency, interval: interval, mode: mode, concurrency: concurrency, notify: notify}
}
func (usecase Usecase) Execute(ctx context.Context, mode string, notify bool, report func(processed, total int)) (domain.HealthSummary, error) {
	testUserID, err := usecase.user()
	if err != nil {
		return domain.HealthSummary{}, err
	}
	channels, err := usecase.store.All()
	if err != nil {
		return domain.HealthSummary{}, err
	}
	if strings.TrimSpace(mode) == "" {
		mode = usecase.mode()
	}
	selected := SelectChannels(channels, mode)
	allowDisable := mode != "passive_recovery"
	concurrency := usecase.concurrency()
	summary := usecase.Perform(ctx, selected, testUserID, allowDisable, concurrency, report)
	if notify && (ctx == nil || ctx.Err() == nil) {
		usecase.notify()
	}
	return summary, nil
}
func (usecase Usecase) HealthCheck(ctx context.Context, channel *domain.Channel, testUserID int, allowDisable bool, disableThreshold int64) domain.HealthSummary {
	summary := domain.HealthSummary{}
	isChannelEnabled := channel.Status == common.ChannelStatusEnabled
	tik := time.Now()
	result := usecase.probe.Test(ctx, channel, testUserID, "", "", usecase.stream(channel))
	milliseconds := time.Since(tik).Milliseconds()
	if ctx.Err() != nil {
		return summary
	}

	summary.Tested++

	shouldBanChannel := false
	newAPIError := result.APIError()
	if newAPIError != nil {
		shouldBanChannel = usecase.shouldDisable(result.APIError())
	}

	if usecase.disableEnabled() && !shouldBanChannel {
		if milliseconds > disableThreshold {
			err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
			newAPIError = types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
			shouldBanChannel = true
		}
	}

	if newAPIError == nil {
		summary.Succeeded++
	} else {
		summary.Failed++
	}

	if allowDisable && isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
		result.HandleFailure(newAPIError)
		summary.Disabled++
	}

	if result.LocalError() == nil && !isChannelEnabled && usecase.shouldEnable(newAPIError, channel.Status) {
		usecase.enable(channel.Id, result.ChannelKey(), channel.Name)
		summary.Enabled++
	}

	usecase.store.SaveResponseTime(channel, milliseconds)
	return summary
}
func (usecase Usecase) Workers(
	ctx context.Context,
	channels []*domain.Channel,
	concurrency int,
	run func(context.Context, *domain.Channel) domain.HealthSummary,
	report func(processed, total int),
) domain.HealthSummary {
	if ctx == nil {
		ctx = context.Background()
	}
	total := len(channels)
	if report != nil {
		report(0, total)
	}
	if total == 0 {
		return domain.HealthSummary{}
	}

	workerCount := min(usecase.normalizeConcurrency(concurrency), total)
	jobs := make(chan *domain.Channel)
	results := make(chan domain.HealthSummary)

	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case channel, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}

					result := domain.HealthSummary{}
					if channel != nil && channel.Status != common.ChannelStatusManuallyDisabled {
						result = run(ctx, channel)
					}

					results <- result

					if usecase.interval() > 0 {
						select {
						case <-ctx.Done():
							return
						case <-time.After(usecase.interval()):
						}
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, channel := range channels {
			select {
			case <-ctx.Done():
				return
			case jobs <- channel:
			}
		}
	}()

	go func() {
		workers.Wait()
		close(results)
	}()

	summary := domain.HealthSummary{}
	processed := 0
	for result := range results {
		summary.Tested += result.Tested
		summary.Succeeded += result.Succeeded
		summary.Failed += result.Failed
		summary.Disabled += result.Disabled
		summary.Enabled += result.Enabled
		processed++
		if report != nil && ctx.Err() == nil {
			report(processed, total)
		}
	}
	return summary
}
func (usecase Usecase) Perform(ctx context.Context, channels []*domain.Channel, testUserID int, allowDisable bool, concurrency int, report func(processed, total int)) domain.HealthSummary {
	if ctx == nil {
		ctx = context.Background()
	}
	disableThreshold := int64(usecase.threshold() * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // an impossible value
	}
	return usecase.Workers(
		ctx,
		channels,
		concurrency,
		func(ctx context.Context, channel *domain.Channel) domain.HealthSummary {
			return usecase.HealthCheck(ctx, channel, testUserID, allowDisable, disableThreshold)
		},
		report,
	)
}
func SelectChannels(channels []*domain.Channel, mode string) []*domain.Channel {
	selected := make([]*domain.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.Status == common.ChannelStatusManuallyDisabled {
			continue
		}
		if mode == "auto_ban_only" && !channel.GetAutoBan() {
			continue
		}
		if mode == "passive_recovery" && channel.Status != common.ChannelStatusAutoDisabled {
			continue
		}
		selected = append(selected, channel)
	}
	return selected
}

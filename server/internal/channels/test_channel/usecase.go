package test_channel

import (
	"time"

	"github.com/QuantumNous/new-api/internal/channels/domain"
)

type Store interface {
	Cached(int) (*domain.Channel, error)
	Get(int, bool) (*domain.Channel, error)
	IsMissing(error) bool
	SaveResponseTime(*domain.Channel, int64)
}
type Usecase struct {
	store Store
	probe domain.Probe
	user  func() (int, error)
	now   func() time.Time
}

func NewUsecase(store Store, probe domain.Probe, user func() (int, error), now func() time.Time) Usecase {
	return Usecase{store: store, probe: probe, user: user, now: now}
}

// Execute reports a failed probe in Output; the error is reserved for
// failures to run it.
func (usecase Usecase) Execute(input Input) (Output, error) {
	channel, err := usecase.store.Cached(input.ID)
	if err != nil {
		channel, err = usecase.store.Get(input.ID, true)
		if usecase.store.IsMissing(err) {
			return Output{}, domain.ErrChannelNotFound
		}
		if err != nil {
			return Output{}, domain.Internal(err)
		}
	}
	userID, err := usecase.user()
	if err != nil {
		return Output{}, domain.Reject(err)
	}
	start := usecase.now()
	result := usecase.probe.Test(input.Context, channel, userID, input.Model, input.Endpoint, input.Stream)
	if result.LocalError() != nil {
		return Output{Kind: "local", Message: result.LocalError().Error(), Response: ChannelTestResponse{Time: 0, ErrorCode: result.ErrorCode()}}, nil
	}
	elapsed := usecase.now().Sub(start).Milliseconds()
	go usecase.store.SaveResponseTime(channel, elapsed)
	duration := float64(elapsed) / 1000.0
	if failure := result.UpstreamError(); failure != nil {
		return Output{Kind: "upstream", Message: failure.Error(), Response: ChannelTestResponse{Time: duration, ErrorCode: result.ErrorCode()}}, nil
	}
	return Output{Response: ChannelTestResponse{Time: duration}}, nil
}

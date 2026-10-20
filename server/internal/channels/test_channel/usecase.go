package test_channel

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"time"
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
func (usecase Usecase) Execute(input Input) (Output, *domain.Failure) {
	channel, err := usecase.store.Cached(input.ID)
	if err != nil {
		channel, err = usecase.store.Get(input.ID, true)
		if usecase.store.IsMissing(err) {
			return Output{}, &domain.Failure{Kind: "missing"}
		}
		if err != nil {
			return Output{}, &domain.Failure{Kind: "internal", Cause: err}
		}
	}
	userID, err := usecase.user()
	if err != nil {
		return Output{}, &domain.Failure{Kind: "actor", Cause: err}
	}
	start := usecase.now()
	result := usecase.probe.Test(input.Context, channel, userID, input.Model, input.Endpoint, input.Stream)
	if result.LocalError() != nil {
		code := ""
		if result.APIError() != nil {
			code = string(result.APIError().GetErrorCode())
		}
		return Output{Kind: "local", Message: result.LocalError().Error(), Response: ChannelTestResponse{Time: 0, ErrorCode: code}}, nil
	}
	elapsed := usecase.now().Sub(start).Milliseconds()
	go usecase.store.SaveResponseTime(channel, elapsed)
	duration := float64(elapsed) / 1000.0
	if result.APIError() != nil {
		return Output{Kind: "upstream", Message: result.APIError().Error(), Response: ChannelTestResponse{Time: duration, ErrorCode: string(result.APIError().GetErrorCode())}}, nil
	}
	return Output{Response: ChannelTestResponse{Time: duration}}, nil
}

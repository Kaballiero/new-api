package refresh_cost_fx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Store interface {
	Load(context.Context, string) error
	Current(string) (domain.ModelCostFXSnapshot, error)
	Save(context.Context, domain.ModelCostFXSnapshot, int64) error
}
type Usecase struct {
	store Store
	fetch func(context.Context) (FXProviderResponse, error)
}

func NewUsecase(store Store, fetch func(context.Context) (FXProviderResponse, error)) Usecase {
	return Usecase{store: store, fetch: fetch}
}
func (u Usecase) Execute(ctx context.Context) (ModelCostFXTaskResult, error) {
	var baseline domain.ModelCostFXSnapshot
	loadedErr := u.store.Load(ctx, domain.ModelCostFXSource)
	if loadedErr == nil {
		var err error
		baseline, err = u.store.Current(domain.ModelCostFXSource)
		if err != nil {
			return ModelCostFXTaskResult{}, err
		}
	} else if !errors.Is(loadedErr, domain.ErrModelCostFXUnavailable) {
		return ModelCostFXTaskResult{}, loadedErr
	} else {
		// An absent row is the only case where version zero is a valid baseline.
		baseline = domain.ModelCostFXSnapshot{Source: domain.ModelCostFXSource, Version: 0}
	}

	provider, err := u.fetch(ctx)
	if err != nil {
		return ModelCostFXTaskResult{}, err
	}
	providerDate, err := time.Parse(time.RFC3339, strings.TrimSpace(provider.Date))
	if err != nil {
		return ModelCostFXTaskResult{}, fmt.Errorf("invalid model cost FX provider date: %w", err)
	}
	now := time.Now()
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return ModelCostFXTaskResult{}, err
	}
	providerCalendarDate := providerDate.In(moscow)
	nowCalendarDate := now.In(moscow)
	providerMidnight := time.Date(providerCalendarDate.Year(), providerCalendarDate.Month(), providerCalendarDate.Day(), 0, 0, 0, 0, moscow)
	nowMidnight := time.Date(nowCalendarDate.Year(), nowCalendarDate.Month(), nowCalendarDate.Day(), 0, 0, 0, 0, moscow)
	if providerMidnight.After(nowMidnight.AddDate(0, 0, 1)) {
		return ModelCostFXTaskResult{}, errors.New("model cost FX provider date is too far in the future")
	}
	usd, ok := provider.Valute["USD"]
	if !ok || !strings.EqualFold(strings.TrimSpace(usd.CharCode), "USD") {
		return ModelCostFXTaskResult{}, errors.New("model cost FX provider response is missing USD")
	}
	if usd.Nominal <= 0 || math.IsNaN(usd.Nominal) || math.IsInf(usd.Nominal, 0) {
		return ModelCostFXTaskResult{}, errors.New("model cost FX provider response has invalid USD nominal")
	}
	if usd.Value <= 0 || math.IsNaN(usd.Value) || math.IsInf(usd.Value, 0) {
		return ModelCostFXTaskResult{}, errors.New("model cost FX provider response has invalid USD value")
	}
	rate := usd.Value / usd.Nominal
	if rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return ModelCostFXTaskResult{}, errors.New("model cost FX provider response has invalid normalized USD")
	}
	fetchedAt := time.Now().Unix()
	if baseline.Version > 0 && baseline.EffectiveAt == providerDate.Unix() && baseline.Rates["USD"] == rate {
		return ModelCostFXTaskResult{Source: domain.ModelCostFXSource, Version: baseline.Version, EffectiveAt: baseline.EffectiveAt, FetchedAt: baseline.FetchedAt}, nil
	}
	candidate := domain.ModelCostFXSnapshot{Source: domain.ModelCostFXSource, EffectiveAt: providerDate.Unix(), FetchedAt: fetchedAt, Rates: map[string]float64{"USD": rate}}
	if err := u.store.Save(ctx, candidate, baseline.Version); err != nil {
		return ModelCostFXTaskResult{}, err
	}
	return ModelCostFXTaskResult{Source: domain.ModelCostFXSource, Version: baseline.Version + 1, EffectiveAt: candidate.EffectiveAt, FetchedAt: candidate.FetchedAt}, nil
}

package extend_deployment

import (
	"time"

	"github.com/QuantumNous/new-api/internal/deployments/domain"
)

type Provider interface {
	ExtendDeployment(string, *ExtendDurationRequest) (*domain.DeploymentDetail, error)
}
type Usecase struct {
	provider func(string) Provider
	now      func() time.Time
}

func NewUsecase(provider func(string) Provider, now func() time.Time) Usecase {
	return Usecase{provider: provider, now: now}
}

func (u Usecase) Execute(input Input) (map[string]any, error) {
	details, err := u.provider(input.APIKey).ExtendDeployment(input.ID, &ExtendDurationRequest{DurationHours: input.DurationHours})
	if err != nil {
		return nil, err
	}
	return domain.DeploymentView(domain.Deployment{ID: details.ID, Status: details.Status, Name: input.ID, CompletedPercent: details.CompletedPercent, HardwareQuantity: details.TotalGPUs, BrandName: details.BrandName, HardwareName: details.HardwareName, ComputeMinutesServed: details.ComputeMinutesServed, ComputeMinutesRemaining: details.ComputeMinutesRemaining, CreatedAt: details.CreatedAt}, u.now), nil
}

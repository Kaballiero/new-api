package rename_deployment

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/deployments/domain"
)

type Provider interface {
	CheckClusterNameAvailability(string) (bool, error)
	UpdateClusterName(string, *UpdateClusterNameRequest) (*UpdateClusterNameResponse, error)
}
type Usecase struct{ provider func(string) Provider }

func NewUsecase(provider func(string) Provider) Usecase { return Usecase{provider: provider} }

func (u Usecase) Execute(input Input) (map[string]any, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, domain.ErrNameEmpty
	}
	client := u.provider(input.APIKey)
	available, err := client.CheckClusterNameAvailability(name)
	if err != nil {
		return nil, fmt.Errorf("failed to check name availability: %w", err)
	}
	if !available {
		return nil, domain.ErrNameUnavailable
	}
	result, err := client.UpdateClusterName(input.ID, &UpdateClusterNameRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": result.Status, "message": result.Message, "id": input.ID, "name": name}, nil
}

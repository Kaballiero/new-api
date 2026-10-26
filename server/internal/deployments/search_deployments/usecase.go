package search_deployments

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/deployments/domain"
)

type Provider interface {
	ListDeployments(*domain.ListDeploymentsOptions) (*domain.DeploymentList, error)
}
type Usecase struct {
	provider func(string) Provider
	now      func() time.Time
}

func NewUsecase(provider func(string) Provider, now func() time.Time) Usecase {
	return Usecase{provider: provider, now: now}
}

func (u Usecase) Execute(input Input) (map[string]any, error) {
	client := u.provider(input.APIKey)
	dl, err := client.ListDeployments(&domain.ListDeploymentsOptions{Status: strings.ToLower(strings.TrimSpace(input.Status)), Page: input.Page, PageSize: input.PageSize, SortBy: "created_at", SortOrder: "desc"})
	if err != nil {
		return nil, err
	}
	selected := dl.Deployments
	total := dl.Total
	keyword := strings.TrimSpace(input.Keyword)
	if keyword != "" {
		selected = make([]domain.Deployment, 0, len(dl.Deployments))
		for _, d := range dl.Deployments {
			if strings.Contains(strings.ToLower(d.Name), strings.ToLower(keyword)) {
				selected = append(selected, d)
			}
		}
		total = len(selected)
	}
	items := make([]map[string]any, 0, len(selected))
	for _, d := range selected {
		items = append(items, domain.DeploymentView(d, u.now))
	}
	out := map[string]any{"page": input.Page, "page_size": input.PageSize, "total": total, "items": items}
	return out, nil
}

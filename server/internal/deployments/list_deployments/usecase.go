package list_deployments

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
	items := make([]map[string]any, 0, len(selected))
	for _, d := range selected {
		items = append(items, domain.DeploymentView(d, u.now))
	}
	out := map[string]any{"page": input.Page, "page_size": input.PageSize, "total": total, "items": items}
	counts := map[string]int64{"all": int64(dl.Total)}
	for _, status := range []string{"running", "completed", "failed", "deployment requested", "termination requested", "destroyed"} {
		counts[status] = 0
	}
	for _, d := range dl.Deployments {
		status := strings.ToLower(strings.TrimSpace(d.Status))
		counts[status]++
	}
	out["status_counts"] = counts
	return out, nil
}

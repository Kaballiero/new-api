package get_uptime_status

import (
	"context"

	"github.com/QuantumNous/new-api/internal/system/domain"
	"golang.org/x/sync/errgroup"
)

type Client interface {
	Fetch(context.Context, map[string]any) domain.UptimeGroupResult
}
type Usecase struct{ client func() Client }

func NewUsecase(client func() Client) Usecase { return Usecase{client: client} }
func (u Usecase) Execute(ctx context.Context, groups []map[string]any) ([]domain.UptimeGroupResult, error) {
	client := u.client()
	results := make([]domain.UptimeGroupResult, len(groups))
	g, gCtx := errgroup.WithContext(ctx)
	for i, group := range groups {
		g.Go(func() error { results[i] = client.Fetch(gCtx, group); return nil })
	}
	g.Wait()
	return results, nil
}

package fetch_batch_results

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	GetBatchJob(int, string) (*domain.Job, error)
	NextResult(int64, int64) (domain.Item, error)
}
type Input struct {
	UserID   int
	PublicID string
}
type Usecase struct {
	store Store
	now   func() time.Time
}

func NewUsecase(s Store, now func() time.Time) Usecase { return Usecase{s, now} }
func (u Usecase) Execute(ctx context.Context, in Input) (*domain.Job, error) {
	job, err := u.store.GetBatchJob(in.UserID, in.PublicID)
	if err != nil {
		return nil, &domain.RuleError{Kind: domain.NotFound}
	}
	if u.now().Unix() >= job.ExpiresAt {
		return nil, &domain.RuleError{Kind: domain.ResultsExpired}
	}
	if job.ResultCount == 0 && !domain.State(job.Status).Terminal() {
		return nil, &domain.RuleError{Kind: domain.ResultsPending}
	}
	return job, nil
}

func (u Usecase) NextResult(batchID, cursor int64) (domain.Item, error) {
	return u.store.NextResult(batchID, cursor)
}

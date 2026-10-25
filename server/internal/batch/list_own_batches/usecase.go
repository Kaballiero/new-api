package list_own_batches

import (
	"context"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	GetBatchJob(int, string) (*domain.Job, error)
	ListBatchJobs(int, int64, int) ([]domain.Job, error)
}
type Input struct {
	UserID, Limit int
	After         string
}
type Result struct {
	Jobs    []domain.Job
	HasMore bool
}
type Usecase struct{ store Store }

func NewUsecase(s Store) Usecase { return Usecase{s} }
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	var result Result
	if in.Limit < 1 || in.Limit > 100 {
		return result, &domain.RuleError{Kind: domain.InvalidLimit}
	}
	var before int64
	if in.After != "" {
		job, err := u.store.GetBatchJob(in.UserID, in.After)
		if err != nil {
			return result, &domain.RuleError{Kind: domain.InvalidCursor}
		}
		before = job.ID
	}
	jobs, err := u.store.ListBatchJobs(in.UserID, before, in.Limit+1)
	if err != nil {
		return result, &domain.RuleError{Kind: domain.StorageListUnavailable}
	}
	result.HasMore = len(jobs) > in.Limit
	if result.HasMore {
		jobs = jobs[:in.Limit]
	}
	result.Jobs = jobs
	return result, nil
}

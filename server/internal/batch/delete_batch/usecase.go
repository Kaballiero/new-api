package delete_batch

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	GetBatchJob(int, string) (*domain.Job, error)
	DeleteLocal(*domain.Job, int64) error
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
func (u Usecase) Execute(ctx context.Context, in Input) (string, error) {
	job, err := u.store.GetBatchJob(in.UserID, in.PublicID)
	if err != nil {
		return "", &domain.RuleError{Kind: domain.NotFound}
	}
	if !domain.State(job.Status).Terminal() || job.BillingStatus != "settled" || job.PurchaseStatus != "settled" {
		return "", &domain.RuleError{Kind: domain.BatchNotReconciled}
	}
	if err := u.store.DeleteLocal(job, u.now().Unix()); err != nil {
		return "", &domain.RuleError{Kind: domain.StorageDeletionUnavailable}
	}
	return job.PublicID, nil
}

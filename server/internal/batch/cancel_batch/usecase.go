package cancel_batch

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	GetBatchJob(int, string) (*domain.Job, error)
	RequestCancel(*domain.Job) error
}
type Provider interface {
	ForChannel(int) (domain.Provider, error)
}
type Input struct {
	UserID   int
	PublicID string
}
type Result struct {
	Job      *domain.Job
	Terminal bool
}
type Usecase struct {
	store    Store
	provider Provider
}

func NewUsecase(s Store, p Provider) Usecase { return Usecase{s, p} }
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	var out Result
	job, err := u.store.GetBatchJob(in.UserID, in.PublicID)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.NotFound}
	}
	p, err := u.provider.ForChannel(job.ChannelType)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.UnsupportedOperation, Cause: err}
	}
	if _, err := p.Cancel(job.UpstreamID); errors.Is(err, domain.ErrUnsupported) {
		return out, &domain.RuleError{Kind: domain.UnsupportedOperation, Cause: err}
	}
	if domain.State(job.Status).Terminal() {
		return Result{job, true}, nil
	}
	if job.SubmitState != "submitted" {
		return out, &domain.RuleError{Kind: domain.SubmissionPending}
	}
	if err := u.store.RequestCancel(job); err != nil {
		return out, &domain.RuleError{Kind: domain.StorageCancellationUnavailable}
	}
	return Result{job, false}, nil
}

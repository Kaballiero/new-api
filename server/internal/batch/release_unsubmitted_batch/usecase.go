package release_unsubmitted_batch

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	domain.UnsubmittedStore
	domain.ReleaseEligibilityStore
	ByPublicID(string) (*domain.Job, error)
	ByID(int64) (*domain.Job, error)
	ClaimBatchJob(int64, string) (bool, error)
	ReleaseClaim(int64, string) error
	ReloadClaim(int64, string) (domain.Job, error)
	MarkOperatorReleased(*domain.Job, string, string) error
}
type Diagnostics interface {
	Error(string)
	Info(string)
}
type Usecase struct {
	store       Store
	codec       domain.Codec
	random      func(int) string
	diagnostics Diagnostics
}

func NewUsecase(s Store, c domain.Codec, random func(int) string, d Diagnostics) Usecase {
	return Usecase{s, c, random, d}
}

type Input struct {
	PublicID   string
	OperatorID int
}
type Result struct {
	Job     domain.Job
	Release domain.ReleaseResult
}

func (u Usecase) Execute(in Input) (result Result, err error) {
	job, err := u.store.ByPublicID(in.PublicID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return result, &domain.RuleError{Kind: domain.ReleaseNotFound}
		}
		u.diagnostics.Info(fmt.Sprintf("Batch %s: root user %d unsubmitted hold release failed: cannot load Batch", in.PublicID, in.OperatorID))
		return result, &domain.RuleError{Kind: domain.ReleaseStorage}
	}
	result.Job = *job
	owner := "batch-release-" + u.random(20)
	claimed, err := u.store.ClaimBatchJob(job.ID, owner)
	if err != nil {
		u.diagnostics.Info(fmt.Sprintf("Batch %s: root user %d unsubmitted hold release failed: cannot claim lease", job.PublicID, in.OperatorID))
		return result, &domain.RuleError{Kind: domain.ReleaseStorage}
	}
	if !claimed {
		u.diagnostics.Info(fmt.Sprintf("Batch %s: root user %d unsubmitted hold release rejected: batch busy", job.PublicID, in.OperatorID))
		return result, &domain.RuleError{Kind: domain.ReleaseBusy}
	}
	defer u.store.ReleaseClaim(job.ID, owner)
	release, err := domain.ReleaseUnsubmitted(u.store, u.codec, u.diagnostics, job, owner, in.OperatorID)
	result.Job = *job
	result.Release = release
	switch {
	case errors.Is(err, domain.ErrBatchReleaseIneligible):
		return result, &domain.RuleError{Kind: domain.ReleaseIneligible}
	case errors.Is(err, domain.ErrBatchConflict):
		return result, &domain.RuleError{Kind: domain.ReleaseConflict}
	case err != nil:
		return result, &domain.RuleError{Kind: domain.ReleaseStorage}
	}
	return result, nil
}

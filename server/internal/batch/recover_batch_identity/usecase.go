package recover_batch_identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	ByPublicID(string) (*domain.Job, error)
	ClaimBatchJob(int64, string) (bool, error)
	ReleaseClaim(int64, string) error
	ReloadClaim(int64, string) (domain.Job, error)
	CountProviderID(int64, string) (int64, error)
	BindRecovery(*domain.Job, string, string, domain.State) error
}
type Input struct{ PublicID, UpstreamID string }
type Usecase struct {
	store   Store
	gateway domain.Gateway
	kinds   domain.ChannelKinds
	random  func(int) string
}

func NewUsecase(s Store, g domain.Gateway, k domain.ChannelKinds, random func(int) string) Usecase {
	return Usecase{s, g, k, random}
}

type Result struct {
	PublicID string
	Previous domain.Job
}

func (u Usecase) Execute(ctx context.Context, in Input) (result Result, err error) {
	if in.UpstreamID == "" || len(in.UpstreamID) > 191 {
		return result, &domain.RuleError{Kind: domain.InvalidRecoveryIdentity}
	}
	job, err := u.store.ByPublicID(in.PublicID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return result, &domain.RuleError{Kind: domain.NotFound}
		}
		return result, &domain.RuleError{Kind: domain.RecoveryStorage}
	}
	result.Previous = *job
	runnerID := "batch-recovery-" + u.random(20)
	claimed, err := u.store.ClaimBatchJob(job.ID, runnerID)
	if err != nil {
		return result, &domain.RuleError{Kind: domain.RecoveryStorage}
	}
	if !claimed {
		return result, &domain.RuleError{Kind: domain.BatchBusy}
	}
	defer u.store.ReleaseClaim(job.ID, runnerID)
	current, err := u.store.ReloadClaim(job.ID, runnerID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return result, &domain.RuleError{Kind: domain.RecoveryConflict}
		}
		return result, &domain.RuleError{Kind: domain.RecoveryStorage}
	}
	*job = current
	result.Previous = current
	err = newVerifier(u.store, u.gateway, u.kinds).Execute(ctx, verificationInput{job, in.UpstreamID, runnerID})
	switch {
	case errors.Is(err, domain.ErrBatchRecoverIneligible):
		return result, &domain.RuleError{Kind: domain.RecoveryIneligible}
	case errors.Is(err, domain.ErrBatchConflict):
		return result, &domain.RuleError{Kind: domain.RecoveryConflict}
	case errors.Is(err, domain.ErrBatchRecoveryStorage):
		return result, &domain.RuleError{Kind: domain.RecoveryStorage}
	case err != nil:
		return result, &domain.RuleError{Kind: domain.RecoveryUnverified}
	}
	result.PublicID = job.PublicID
	return result, nil
}

type verifier struct {
	store   Store
	gateway domain.Gateway
	kinds   domain.ChannelKinds
}
type verificationInput struct {
	Job                  *domain.Job
	UpstreamID, RunnerID string
}

func newVerifier(s Store, g domain.Gateway, k domain.ChannelKinds) verifier { return verifier{s, g, k} }
func (u verifier) Execute(ctx context.Context, in verificationInput) error {
	job, upstreamID, runnerID := in.Job, in.UpstreamID, in.RunnerID
	if !domain.RecoverEligible(job, func(kind int) bool { _, err := u.gateway.ForChannel(kind); return err == nil }) {
		return domain.ErrBatchRecoverIneligible
	}
	adapter, err := u.gateway.ForChannel(job.ChannelType)
	if err != nil {
		return err
	}
	request, err := adapter.Poll(upstreamID)
	if err != nil {
		return err
	}
	candidate := *job
	candidate.UpstreamID = upstreamID
	response, err := u.gateway.Request(ctx, &candidate, request, nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("recovery retrieval HTTP %d", response.StatusCode)
	}
	verified := 0
	var storageErr error
	verify := func(result domain.Result) error {
		count, err := u.store.CountProviderID(job.ID, result.CustomID)
		if err != nil {
			storageErr = err
			return err
		}
		if count != 1 {
			return errors.New("recovery result does not belong to this Batch")
		}
		verified++
		return nil
	}
	snapshot, err := adapter.ReadStatus(response.Body, verify)
	if storageErr != nil {
		return fmt.Errorf("%w: %v", domain.ErrBatchRecoveryStorage, storageErr)
	}
	if err != nil {
		return err
	}
	if snapshot.ID != upstreamID {
		return errors.New("recovery batch identity mismatch")
	}
	if verified == 0 && snapshot.State.Terminal() {
		var files []string
		switch job.ChannelType {
		case u.kinds.Anthropic:
			files = []string{""}
		case u.kinds.OpenAI:
			if snapshot.OutputFileID != "" {
				files = append(files, snapshot.OutputFileID)
			}
			if snapshot.ErrorFileID != "" {
				files = append(files, snapshot.ErrorFileID)
			}
		case u.kinds.Google:
			if snapshot.OutputFileID != "" {
				files = []string{snapshot.OutputFileID}
			}
		}
		for _, file := range files {
			request, err := u.gateway.ResultsRequest(&candidate, file)
			if err != nil {
				return err
			}
			response, err := u.gateway.Request(ctx, &candidate, request, nil, "")
			if err != nil {
				return err
			}
			if response.StatusCode != 200 {
				response.Body.Close()
				return fmt.Errorf("recovery results HTTP %d", response.StatusCode)
			}
			err = adapter.ReadResults(response.Body, verify)
			response.Body.Close()
			if storageErr != nil {
				return fmt.Errorf("%w: %v", domain.ErrBatchRecoveryStorage, storageErr)
			}
			if err != nil {
				return err
			}
		}
	}
	if verified == 0 {
		return errors.New("provider has not returned request identities proving ownership; keep submission ambiguous")
	}
	err = u.store.BindRecovery(job, upstreamID, runnerID, snapshot.State)
	if err != nil && !errors.Is(err, domain.ErrBatchConflict) {
		return fmt.Errorf("%w: %v", domain.ErrBatchRecoveryStorage, err)
	}
	return err
}

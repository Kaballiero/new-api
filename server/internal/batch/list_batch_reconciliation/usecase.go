package list_batch_reconciliation

import (
	"time"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

type Store interface {
	domain.ReleaseEligibilityStore
	ReconciliationCursor(string) (int64, error)
	ListReconciliationJobs(int64, int) ([]domain.Job, error)
}
type Input struct {
	Before string
	Limit  int
}
type Usecase struct {
	store   Store
	gateway domain.Gateway
	codec   domain.Codec
	kinds   domain.ChannelKinds
	now     func() time.Time
}

func NewUsecase(s Store, g domain.Gateway, c domain.Codec, k domain.ChannelKinds, now func() time.Time) Usecase {
	return Usecase{s, g, c, k, now}
}

// ListBatchReconciliation returns the operator view after the Batch with
// public id before, newest first.
func (u Usecase) Execute(in Input) ([]Entry, bool, error) {
	before, limit := in.Before, in.Limit
	var cursor int64
	if before != "" {
		id, err := u.store.ReconciliationCursor(before)
		if err != nil {
			return nil, false, err
		}
		cursor = id
	}
	jobs, err := u.store.ListReconciliationJobs(cursor, limit+1)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	now := u.now()
	entries := make([]Entry, len(jobs))
	for i := range jobs {
		job := &jobs[i]
		canRelease, err := domain.ReleaseEligible(u.store, job)
		if err != nil {
			return nil, false, err
		}
		entries[i] = Entry{ID: job.PublicID, UserID: job.UserID, TokenID: job.TokenID, ChannelID: job.ChannelID, Model: job.Model, UpstreamModel: job.UpstreamModel, Status: job.Status, SubmitState: job.SubmitState, BillingStatus: job.BillingStatus, PurchaseStatus: job.PurchaseStatus, UpstreamID: job.UpstreamID, RequestCount: job.RequestCount, ResultCount: job.ResultCount, SettledCount: job.SettledCount, ReservedQuota: job.ReservedQuota, ChargedQuota: job.ChargedQuota, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Reason: domain.ReconciliationReason(job, now, u.kinds, u.codec), CanRelease: canRelease, CanRecover: domain.RecoverEligible(job, func(kind int) bool { _, err := u.gateway.ForChannel(kind); return err == nil })}
		var diagnostic domain.Failure
		if len(job.LastError) <= 4096 && u.codec.UnmarshalJsonStr(job.LastError, &diagnostic) == nil && diagnostic.Version == 1 {
			entries[i].Diagnostic = &diagnostic
		}
	}
	return entries, hasMore, nil
}

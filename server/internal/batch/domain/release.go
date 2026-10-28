package domain

import (
	"errors"
	"fmt"
)

type ReleaseStore interface {
	UnsubmittedStore
	ReleaseEligibilityStore
	ReloadClaim(int64, string) (Job, error)
	ByID(int64) (*Job, error)
	MarkOperatorReleased(*Job, string, string) error
}
type ReleaseDiagnostics interface {
	Error(string)
	Info(string)
}

func ReleaseUnsubmitted(store ReleaseStore, codec Codec, diagnostics ReleaseDiagnostics, job *Job, runnerID string, operatorID int) (result ReleaseResult, err error) {
	defer func() {
		outcome := "released"
		switch {
		case err != nil:
			outcome = "rejected: " + err.Error()
		case result.Pending:
			outcome = "release pending cache confirmation"
		case result.AlreadyReleased:
			outcome = "already released"
		}
		diagnostics.Info(fmt.Sprintf("Batch %s: root user %d unsubmitted hold release %s; released quota %d", job.PublicID, operatorID, outcome, result.ReleasedQuota))
	}()
	current, loadErr := store.ReloadClaim(job.ID, runnerID)
	if loadErr != nil {
		if errors.Is(loadErr, ErrNotFound) {
			loadErr = ErrBatchConflict
		}
		return result, loadErr
	}
	*job = current

	result.PreviousLastError = job.LastError
	var previous Failure
	if job.SubmitState == "rejected" && job.UpstreamID == "" && codec.UnmarshalJsonStr(job.LastError, &previous) == nil && previous.Code == OperatorReleasedCode {
		result.AlreadyReleased = true
		if job.BillingStatus != "settled" {
			if err = RejectUnsubmitted(store, diagnostics.Error, job, job.LastError); err != nil {
				return result, err
			}
			reloaded, loadErr := store.ByID(job.ID)
			if loadErr != nil {
				return result, loadErr
			}
			*job = *reloaded
			result.Pending = job.BillingStatus != "settled" || job.ReservedQuota != 0
		}
		return result, nil
	}
	eligible, err := ReleaseEligible(store, job)
	if err != nil {
		return result, err
	}
	if !eligible {
		return result, ErrBatchReleaseIneligible
	}
	held := job.ReservedQuota
	encoded, err := codec.Marshal(Failure{Version: 1, Phase: "reconciliation", Code: OperatorReleasedCode, Message: "Operator confirmed no provider Batch exists; hold released."})
	if err != nil {
		return result, err
	}
	if err := store.MarkOperatorReleased(job, runnerID, string(encoded)); err != nil {
		return result, err
	}
	job.SubmitState, job.LastError = "rejected", string(encoded)
	if err = RejectUnsubmitted(store, diagnostics.Error, job, job.LastError); err != nil {
		return result, err
	}
	reloaded, loadErr := store.ByID(job.ID)
	if loadErr != nil {
		return result, loadErr
	}
	*job = *reloaded
	if job.BillingStatus != "settled" || job.ReservedQuota != 0 {
		result.Pending = true
		return result, nil
	}
	result.ReleasedQuota = held
	return result, nil
}

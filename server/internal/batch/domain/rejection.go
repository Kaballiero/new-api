package domain

import "errors"

type UnsubmittedStore interface {
	RejectUnsubmitted(*Job, string) error
	UnsettledIdentities(int64) ([]Item, error)
	SettleBatchItem(int64, string, int, string) error
	SyncBatchQuotaEvents(int) error
	ReleaseSettledBatchHolds(int64) error
}

func RejectUnsubmitted(store UnsubmittedStore, diagnostics func(string), job *Job, reason string) error {
	if job.SubmitState != "queued" && job.SubmitState != "preparing" && job.SubmitState != "rejected" {
		return errors.New("cannot refund an ambiguous or submitted batch")
	}
	if err := store.RejectUnsubmitted(job, reason); err != nil {
		return err
	}
	job.Status, job.LastError = "failed", reason
	items, err := store.UnsettledIdentities(job.ID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := store.SettleBatchItem(job.ID, item.CustomID, 0, `{"purchase_status":"unbilled","purchase_usd":0,"reason":"batch_not_submitted"}`); err != nil {
			return err
		}
	}
	if err := store.SyncBatchQuotaEvents(1000); err != nil {
		diagnostics("Batch outbox: " + err.Error())
	}
	return store.ReleaseSettledBatchHolds(job.ID)
}

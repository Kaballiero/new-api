package domain

import (
	"errors"
	"fmt"
)

type UnbilledStore interface {
	UnsettledIdentities(int64) ([]Item, error)
	SettleBatchItem(int64, string, int, string) error
	SyncBatchQuotaEvents(int) error
	ReleaseSettledBatchHolds(int64) error
}

type UnsubmittedStore interface {
	UnbilledStore
	RejectUnsubmitted(*Job, string) error
}

func RejectUnsubmitted(store UnsubmittedStore, diagnostics func(string), job *Job, reason string) error {
	if job.SubmitState != "queued" && job.SubmitState != "preparing" && job.SubmitState != "rejected" {
		return errors.New("cannot refund an ambiguous or submitted batch")
	}
	if err := store.RejectUnsubmitted(job, reason); err != nil {
		return err
	}
	job.Status, job.LastError = "failed", reason
	return SettleUnbilled(store, diagnostics, job.ID, "batch_not_submitted")
}

// SettleUnbilled settles every unsettled item of a Batch with an unbilled zero
// receipt naming reason, then releases the hold once its cache events apply.
// Replaying it is safe: settled items are skipped and receipts are identical.
func SettleUnbilled(store UnbilledStore, diagnostics func(string), batchID int64, reason string) error {
	items, err := store.UnsettledIdentities(batchID)
	if err != nil {
		return err
	}
	receipt := fmt.Sprintf(`{"purchase_status":"unbilled","purchase_usd":0,"reason":%q}`, reason)
	for _, item := range items {
		if err := store.SettleBatchItem(batchID, item.CustomID, 0, receipt); err != nil {
			return err
		}
	}
	if err := store.SyncBatchQuotaEvents(1000); err != nil {
		diagnostics("Batch outbox: " + err.Error())
	}
	return store.ReleaseSettledBatchHolds(batchID)
}

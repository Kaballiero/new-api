package domain

type ErrorKind string

const (
	BatchBusy                      ErrorKind = "batch_busy/BatchBusy"
	BatchNotReconciled             ErrorKind = "batch_not_reconciled/BatchNotReconciled"
	BatchPricingUnavailable        ErrorKind = "batch_pricing_unavailable/BatchPricingUnavailable"
	BatchQuotaOverflow             ErrorKind = "batch_quota_overflow/BatchQuotaOverflow"
	BatchTooLarge                  ErrorKind = "batch_too_large/BatchTooLarge"
	BatchWorkerUnavailable         ErrorKind = "batch_worker_unavailable/BatchWorkerUnavailable"
	ChannelSnapshotUnavailable     ErrorKind = "channel_unavailable/ChannelSnapshotUnavailable"
	ChannelUnavailable             ErrorKind = "channel_unavailable/ChannelUnavailable"
	IdempotencyConflict            ErrorKind = "idempotency_conflict/IdempotencyConflict"
	InsufficientQuota              ErrorKind = "insufficient_quota/InsufficientQuota"
	InvalidBatchPrice              ErrorKind = "invalid_batch_price/InvalidBatchPrice"
	InvalidChannelHeaders          ErrorKind = "invalid_channel_headers/InvalidChannelHeaders"
	InvalidChannelParameters       ErrorKind = "invalid_channel_parameters/InvalidChannelParameters"
	InvalidCursor                  ErrorKind = "invalid_cursor/InvalidCursor"
	InvalidCustomId                ErrorKind = "invalid_custom_id/InvalidCustomId"
	InvalidEnvelope                ErrorKind = "invalid_request/InvalidEnvelope"
	InvalidIdempotencyKey          ErrorKind = "invalid_idempotency_key/InvalidIdempotencyKey"
	InvalidItem                    ErrorKind = "invalid_request/InvalidItem"
	InvalidLimit                   ErrorKind = "invalid_limit/InvalidLimit"
	InvalidModelMapping            ErrorKind = "invalid_model_mapping/InvalidModelMapping"
	InvalidPayload                 ErrorKind = "invalid_request/InvalidPayload"
	InvalidProvider                ErrorKind = "invalid_provider/InvalidProvider"
	InvalidPurchaseCost            ErrorKind = "invalid_purchase_cost/InvalidPurchaseCost"
	InvalidPurchaseEvidence        ErrorKind = "invalid_request/InvalidPurchaseEvidence"
	InvalidRecoveryIdentity        ErrorKind = "invalid_request/InvalidRecoveryIdentity"
	MalformedEnvelope              ErrorKind = "invalid_request/MalformedEnvelope"
	NotFound                       ErrorKind = "not_found/NotFound"
	PricingUnavailable             ErrorKind = "pricing_unavailable/PricingUnavailable"
	PurchaseReconciliationConflict ErrorKind = "purchase_reconciliation_conflict/PurchaseReconciliationConflict"
	RecoveryUnverified             ErrorKind = "recovery_unverified/RecoveryUnverified"
	ResultsExpired                 ErrorKind = "results_expired/ResultsExpired"
	ResultsPending                 ErrorKind = "results_pending/ResultsPending"
	StorageCancellationUnavailable ErrorKind = "storage_unavailable/StorageCancellationUnavailable"
	StorageDeletionUnavailable     ErrorKind = "storage_unavailable/StorageDeletionUnavailable"
	StorageListUnavailable         ErrorKind = "storage_unavailable/StorageListUnavailable"
	StorageLookupUnavailable       ErrorKind = "storage_unavailable/StorageLookupUnavailable"
	StorageReservationUnavailable  ErrorKind = "storage_unavailable/StorageReservationUnavailable"
	SubmissionPending              ErrorKind = "submission_pending/SubmissionPending"
	UnsupportedOperation           ErrorKind = "unsupported_operation/UnsupportedOperation"
)

// RuleError describes a rejected business operation; HTTP mapping belongs to transport.
type RuleError struct {
	Kind            ErrorKind
	ItemID          string
	Field, Endpoint string
	Limit           int
	Cause           error
}

func (e *RuleError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}
func (e *RuleError) Unwrap() error { return e.Cause }

// Nested validation/pricing failures carry data; transport owns their wording.
const (
	ItemBodyInvalid                     ErrorKind = "batch.ItemBodyInvalid"
	ItemModelMismatch                   ErrorKind = "batch.ItemModelMismatch"
	ItemStreamingUnsupported            ErrorKind = "batch.ItemStreamingUnsupported"
	ItemCountInvalid                    ErrorKind = "batch.ItemCountInvalid"
	OutputLimitUnsupported              ErrorKind = "batch.OutputLimitUnsupported"
	OutputLimitOutOfRange               ErrorKind = "batch.OutputLimitOutOfRange"
	UseGenerationConfig                 ErrorKind = "batch.UseGenerationConfig"
	GenerationConfigInvalid             ErrorKind = "batch.GenerationConfigInvalid"
	UseMaxOutputTokens                  ErrorKind = "batch.UseMaxOutputTokens"
	UseCandidateCount                   ErrorKind = "batch.UseCandidateCount"
	MaxOutputTokensInvalid              ErrorKind = "batch.MaxOutputTokensInvalid"
	CandidateCountInvalid               ErrorKind = "batch.CandidateCountInvalid"
	OutputLimitRequired                 ErrorKind = "batch.OutputLimitRequired"
	BillingExpressionRequired           ErrorKind = "batch.BillingExpressionRequired"
	BillingExpressionHeadersUnsupported ErrorKind = "batch.BillingExpressionHeadersUnsupported"
	BillingExpressionInvalid            ErrorKind = "batch.BillingExpressionInvalid"
	OpenRouterAdaptorUnsupported        ErrorKind = "batch.OpenRouterAdaptorUnsupported"
)

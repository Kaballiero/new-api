package transport

import (
	"errors"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/batch/domain"

	"github.com/gin-gonic/gin"
)

type responseError struct {
	Status          int
	Code, MessageID string
	Message         string
}

var responses = map[domain.ErrorKind]responseError{
	domain.ReleaseNotFound:             {Status: 404, Code: "not_found", Message: "batch not found"},
	domain.ReleaseBusy:                 {Status: 409, Code: "batch_busy", Message: "batch reconciliation is already in progress"},
	domain.ReconciliationInvalidLimit:  {Status: 400, Code: "invalid_limit", Message: "limit must be in 1..100"},
	domain.ReconciliationInvalidCursor: {Status: 400, Code: "invalid_cursor", Message: "unknown batch cursor"},
	domain.ReconciliationStorage:       {Status: 503, Code: "storage_unavailable", Message: "batch storage unavailable"},
	domain.RecoveryIneligible:          {Status: 409, Code: "recovery_ineligible", Message: "only an unknown creation without provider identity can be recovered"},
	domain.RecoveryConflict:            {Status: 409, Code: "batch_conflict", Message: "Batch state changed during recovery; reload and retry"},
	domain.RecoveryStorage:             {Status: 503, Code: "storage_unavailable", Message: "cannot recover Batch"},
	domain.ReleaseIneligible:           {Status: 409, Code: "release_ineligible", Message: "only an unknown creation without provider identity, results or charges can be released"},
	domain.ReleaseConflict:             {Status: 409, Code: "batch_conflict", Message: "Batch state changed during release; reload and retry"},
	domain.ReleaseStorage:              {Status: 503, Code: "storage_unavailable", Message: "cannot release Batch hold"},
	domain.InvalidReleaseEvidence:      {Status: 400, Code: "invalid_request", Message: "non-empty evidence of at most 4096 characters is required"},
	domain.BatchBusy:                   {Status: 409, Code: "batch_busy", MessageID: i18n.MsgBatchBatchBusy},
	domain.BatchNotReconciled:          {Status: 409, Code: "batch_not_reconciled", MessageID: i18n.MsgBatchBatchNotReconciled},

	domain.BatchPricingUnavailable:    {Status: 503, Code: "batch_pricing_unavailable"},
	domain.BatchQuotaOverflow:         {Status: 400, Code: "batch_quota_overflow", MessageID: i18n.MsgBatchBatchQuotaOverflow},
	domain.BatchTooLarge:              {Status: 400, Code: "batch_too_large", MessageID: i18n.MsgBatchBatchTooLarge},
	domain.BatchWorkerUnavailable:     {Status: 503, Code: "batch_worker_unavailable", MessageID: i18n.MsgBatchBatchWorkerUnavailable},
	domain.ChannelSnapshotUnavailable: {Status: 503, Code: "channel_unavailable", MessageID: i18n.MsgBatchChannelSnapshotUnavailable},
	domain.ChannelUnavailable:         {Status: 503, Code: "channel_unavailable", MessageID: i18n.MsgBatchChannelUnavailable},
	domain.IdempotencyConflict:        {Status: 409, Code: "idempotency_conflict", MessageID: i18n.MsgBatchIdempotencyConflict},
	domain.InsufficientQuota:          {Status: 403, Code: "insufficient_quota"},
	domain.InvalidBatchPrice:          {Status: 400, Code: "invalid_batch_price"},
	domain.InvalidChannelHeaders:      {Status: 400, Code: "invalid_channel_headers"},
	domain.InvalidChannelParameters:   {Status: 400, Code: "invalid_channel_parameters"},
	domain.InvalidCursor:              {Status: 400, Code: "invalid_cursor", MessageID: i18n.MsgBatchInvalidCursor},
	domain.InvalidCustomId:            {Status: 400, Code: "invalid_custom_id", MessageID: i18n.MsgBatchInvalidCustomId},
	domain.InvalidEnvelope:            {Status: 400, Code: "invalid_request", MessageID: i18n.MsgBatchInvalidEnvelope},
	domain.InvalidIdempotencyKey:      {Status: 400, Code: "invalid_idempotency_key", MessageID: i18n.MsgBatchInvalidIdempotencyKey},
	domain.InvalidItem:                {Status: 400, Code: "invalid_request"},
	domain.InvalidLimit:               {Status: 400, Code: "invalid_limit", MessageID: i18n.MsgBatchInvalidLimit},
	domain.InvalidModelMapping:        {Status: 400, Code: "invalid_model_mapping"},
	domain.InvalidPayload:             {Status: 400, Code: "invalid_request"},
	domain.InvalidProvider:            {Status: 400, Code: "invalid_provider"},
	domain.InvalidPurchaseCost:        {Status: 400, Code: "invalid_purchase_cost", MessageID: i18n.MsgBatchInvalidPurchaseCost},
	domain.InvalidPurchaseEvidence:    {Status: 400, Code: "invalid_request", MessageID: i18n.MsgBatchInvalidPurchaseEvidence},
	domain.InvalidRecoveryIdentity:    {Status: 400, Code: "invalid_request", MessageID: i18n.MsgBatchInvalidRecoveryIdentity},
	domain.MalformedEnvelope:          {Status: 400, Code: "invalid_request", MessageID: i18n.MsgBatchMalformedEnvelope},
	domain.NotFound:                   {Status: 404, Code: "not_found", MessageID: i18n.MsgBatchNotFound},
	domain.PricingUnavailable:         {Status: 503, Code: "pricing_unavailable", MessageID: i18n.MsgBatchPricingUnavailable},
	domain.PurchaseReconciliationConflict: {Status: 409, Code: "purchase_reconciliation_conflict",
		MessageID: i18n.MsgBatchPurchaseReconciliationConflict},
	domain.RecoveryUnverified: {Status: 409, Code: "recovery_unverified", MessageID: i18n.MsgBatchRecoveryUnverified},

	domain.ResultsExpired:                 {Status: 410, Code: "results_expired", MessageID: i18n.MsgBatchResultsExpired},
	domain.ResultsPending:                 {Status: 409, Code: "results_pending", MessageID: i18n.MsgBatchResultsPending},
	domain.StorageCancellationUnavailable: {Status: 503, Code: "storage_unavailable", MessageID: i18n.MsgBatchStorageCancellationUnavailable},
	domain.StorageDeletionUnavailable:     {Status: 503, Code: "storage_unavailable", MessageID: i18n.MsgBatchStorageDeletionUnavailable},
	domain.StorageListUnavailable:         {Status: 503, Code: "storage_unavailable", MessageID: i18n.MsgBatchStorageListUnavailable},
	domain.StorageLookupUnavailable:       {Status: 503, Code: "storage_unavailable", MessageID: i18n.MsgBatchStorageLookupUnavailable},
	domain.StorageReservationUnavailable:  {Status: 503, Code: "storage_unavailable", MessageID: i18n.MsgBatchStorageReservationUnavailable},
	domain.SubmissionPending:              {Status: 409, Code: "submission_pending", MessageID: i18n.MsgBatchSubmissionPending},
	domain.UnsupportedOperation:           {Status: 400, Code: "unsupported_operation"},
}

func Error(c *gin.Context, err error) string {
	var failure *domain.RuleError
	if !errors.As(err, &failure) {
		panic(err)
	}
	response, ok := responses[failure.Kind]
	if !ok {
		panic(err)
	}
	message := response.Message
	if response.MessageID != "" {
		message = i18n.T(c, response.MessageID)
	}
	if failure.Kind == domain.InvalidItem {
		message = i18n.T(c, i18n.MsgBatchInvalidItem, map[string]any{"ItemID": failure.ItemID, "Error": causeMessage(c, failure.Cause)})
	} else if failure.Cause != nil {
		message = causeMessage(c, failure.Cause)
	}
	c.AbortWithStatusJSON(response.Status, gin.H{"error": gin.H{"type": "batch_error", "code": response.Code, "message": message}})
	return response.Code
}

var detailMessages = map[domain.ErrorKind]string{
	domain.ItemBodyInvalid:                     i18n.MsgBatchItemBodyInvalid,
	domain.ItemModelMismatch:                   i18n.MsgBatchItemModelMismatch,
	domain.ItemStreamingUnsupported:            i18n.MsgBatchItemStreamingUnsupported,
	domain.ItemCountInvalid:                    i18n.MsgBatchItemCountInvalid,
	domain.OutputLimitUnsupported:              i18n.MsgBatchOutputLimitUnsupported,
	domain.OutputLimitOutOfRange:               i18n.MsgBatchOutputLimitOutOfRange,
	domain.UseGenerationConfig:                 i18n.MsgBatchUseGenerationConfig,
	domain.GenerationConfigInvalid:             i18n.MsgBatchGenerationConfigInvalid,
	domain.UseMaxOutputTokens:                  i18n.MsgBatchUseMaxOutputTokens,
	domain.UseCandidateCount:                   i18n.MsgBatchUseCandidateCount,
	domain.MaxOutputTokensInvalid:              i18n.MsgBatchMaxOutputTokensInvalid,
	domain.CandidateCountInvalid:               i18n.MsgBatchCandidateCountInvalid,
	domain.OutputLimitRequired:                 i18n.MsgBatchOutputLimitRequired,
	domain.BillingExpressionRequired:           i18n.MsgBatchBillingExpressionRequired,
	domain.BillingExpressionHeadersUnsupported: i18n.MsgBatchBillingExpressionHeadersUnsupported,
	domain.BillingExpressionInvalid:            i18n.MsgBatchBillingExpressionInvalid,
	domain.OpenRouterAdaptorUnsupported:        i18n.MsgBatchOpenRouterAdaptorUnsupported,
}

func causeMessage(c *gin.Context, err error) string {
	if err == nil {
		return "<nil>"
	}
	var detail *domain.RuleError
	if errors.As(err, &detail) {
		if key, ok := detailMessages[detail.Kind]; ok {
			data := map[string]any{"Field": detail.Field, "Endpoint": detail.Endpoint, "Limit": detail.Limit}
			if detail.Cause != nil {
				data["Error"] = detail.Cause.Error()
			}
			return i18n.T(c, key, data)
		}
	}
	if errors.Is(err, domain.ErrBatchInsufficientQuota) {
		return i18n.T(c, i18n.MsgBatchInsufficientQuota)
	}
	if errors.Is(err, domain.ErrUnsupported) {
		return i18n.T(c, i18n.MsgBatchUnsupportedOperation)
	}
	// Shared provider/pricing/parser errors belong to their existing owners.
	return err.Error()
}

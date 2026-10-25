package transport

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/gin-gonic/gin"
)

type responseError struct {
	Status        int
	Code, Message string
}

var responses = map[domain.ErrorKind]responseError{
	domain.BatchBusy:          {Status: 409, Code: "batch_busy", Message: "batch reconciliation is already in progress"},
	domain.BatchNotReconciled: {Status: 409, Code: "batch_not_reconciled", Message: "only terminal, financially settled batches can be deleted"},

	domain.BatchPricingUnavailable:    {Status: 503, Code: "batch_pricing_unavailable"},
	domain.BatchQuotaOverflow:         {Status: 400, Code: "batch_quota_overflow", Message: "batch reservation exceeds the wallet quota limit"},
	domain.BatchTooLarge:              {Status: 400, Code: "batch_too_large", Message: "prepared batch exceeds 16 MiB"},
	domain.BatchWorkerUnavailable:     {Status: 503, Code: "batch_worker_unavailable", Message: "Batch polling must be enabled"},
	domain.ChannelSnapshotUnavailable: {Status: 503, Code: "channel_unavailable", Message: "cannot capture channel configuration"},
	domain.ChannelUnavailable:         {Status: 503, Code: "channel_unavailable", Message: "selected channel is unavailable"},
	domain.IdempotencyConflict:        {Status: 409, Code: "idempotency_conflict", Message: "Idempotency-Key was used with a different batch"},
	domain.InsufficientQuota:          {Status: 403, Code: "insufficient_quota"},
	domain.InvalidBatchPrice:          {Status: 400, Code: "invalid_batch_price"},
	domain.InvalidChannelHeaders:      {Status: 400, Code: "invalid_channel_headers"},
	domain.InvalidChannelParameters:   {Status: 400, Code: "invalid_channel_parameters"},
	domain.InvalidCursor:              {Status: 400, Code: "invalid_cursor", Message: "unknown batch cursor"},
	domain.InvalidCustomId:            {Status: 400, Code: "invalid_custom_id", Message: "custom_id must be unique and match [a-zA-Z0-9_-]{1,64}"},
	domain.InvalidEnvelope:            {Status: 400, Code: "invalid_request", Message: "model and 1..1000 requests are required"},
	domain.InvalidIdempotencyKey:      {Status: 400, Code: "invalid_idempotency_key", Message: "Idempotency-Key must not exceed 256 bytes"},
	domain.InvalidItem:                {Status: 400, Code: "invalid_request"},
	domain.InvalidLimit:               {Status: 400, Code: "invalid_limit", Message: "limit must be in 1..100"},
	domain.InvalidModelMapping:        {Status: 400, Code: "invalid_model_mapping"},
	domain.InvalidPayload:             {Status: 400, Code: "invalid_request"},
	domain.InvalidProvider:            {Status: 400, Code: "invalid_provider"},
	domain.InvalidPurchaseCost:        {Status: 400, Code: "invalid_purchase_cost", Message: "invalid procurement USD amount"},
	domain.InvalidPurchaseEvidence:    {Status: 400, Code: "invalid_request", Message: "bounded cost_usd and procurement evidence are required"},
	domain.InvalidRecoveryIdentity:    {Status: 400, Code: "invalid_request", Message: "a bounded upstream_id is required"},
	domain.MalformedEnvelope:          {Status: 400, Code: "invalid_request", Message: "invalid or oversized Batch JSON"},
	domain.NotFound:                   {Status: 404, Code: "not_found", Message: "batch not found"},
	domain.PricingUnavailable:         {Status: 503, Code: "pricing_unavailable", Message: "cannot capture Batch pricing"},
	domain.PurchaseReconciliationConflict: {Status: 409, Code: "purchase_reconciliation_conflict",
		Message: "Batch is unavailable, not terminal, or already reconciled"},
	domain.RecoveryUnverified: {Status: 409, Code: "recovery_unverified", Message: "provider ownership could not be verified; Batch remains unchanged"},

	domain.ResultsExpired:                 {Status: 410, Code: "results_expired", Message: "batch results have expired"},
	domain.ResultsPending:                 {Status: 409, Code: "results_pending", Message: "batch results are not available yet"},
	domain.StorageCancellationUnavailable: {Status: 503, Code: "storage_unavailable", Message: "cannot request cancellation"},
	domain.StorageDeletionUnavailable:     {Status: 503, Code: "storage_unavailable", Message: "cannot delete batch"},
	domain.StorageListUnavailable:         {Status: 503, Code: "storage_unavailable", Message: "batch storage unavailable"},
	domain.StorageLookupUnavailable:       {Status: 503, Code: "storage_unavailable", Message: "batch storage is unavailable"},
	domain.StorageReservationUnavailable:  {Status: 503, Code: "storage_unavailable", Message: "could not reserve Batch quota"},
	domain.SubmissionPending:              {Status: 409, Code: "submission_pending", Message: "cannot cancel before upstream identity is confirmed"},
	domain.UnsupportedOperation:           {Status: 400, Code: "unsupported_operation"},
}

func Error(c *gin.Context, err error) {
	var failure *domain.RuleError
	if !errors.As(err, &failure) {
		panic(err)
	}
	response, ok := responses[failure.Kind]
	if !ok {
		panic(err)
	}
	message := response.Message
	if failure.Kind == domain.InvalidItem {
		message = fmt.Sprintf("request %s: %v", failure.ItemID, failure.Cause)
	} else if failure.Cause != nil {
		message = failure.Cause.Error()
	}
	c.AbortWithStatusJSON(response.Status, gin.H{"error": gin.H{"type": "batch_error", "code": response.Code, "message": message}})
}

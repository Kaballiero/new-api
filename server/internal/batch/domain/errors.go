package domain

import (
	"errors"

	"github.com/QuantumNous/new-api/pkg/batchstate"
)

var ErrBatchInsufficientQuota = errors.New("insufficient quota for batch reservation")
var ErrBatchConflict = batchstate.ErrConflict
var ErrBatchReservationPending = errors.New("batch reservation awaits cache confirmation")
var ErrBatchDispatchDenied = errors.New("batch dispatch authorization unavailable")
var ErrBatchReservationUnsafe = errors.New("batch reservation ownership or holds are inconsistent")
var ErrNotFound = errors.New("record not found")
var ErrUnsupported = errors.New("batch operation is not supported by this provider")

const BatchSettlementOpenRouterCostV1 = "openrouter_cost_v1"
const BatchSettlementOpenRouterJobCostV2 = "openrouter_job_cost_v2"

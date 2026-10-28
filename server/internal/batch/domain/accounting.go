package domain

import (
	"errors"
	"fmt"
	"math"

	billing "github.com/QuantumNous/new-api/internal/billing/domain"
)

// Accounts contains only facts already read under the transaction's locks.
type Accounts struct {
	UserID, TokenID, TokenUserID                             int
	UserQuota, UserUsed, UserRequests, TokenQuota, TokenUsed int
	UserHeld, TokenHeld                                      int64
	UserDeleted, TokenDeleted                                bool
	UserStatus, TokenStatus                                  int
	TokenExpires                                             int64
}

func ValidateReservationOwnership(job Job, a Accounts) error {
	if a.UserID != job.UserID || a.TokenID != job.TokenID || a.TokenUserID != job.UserID ||
		job.ReservedQuota < 0 || a.UserHeld < job.ReservedQuota || a.TokenHeld < job.ReservedQuota {
		return ErrBatchReservationUnsafe
	}
	return nil
}
func DispatchRevoked(a Accounts, now int64, userEnabled, tokenEnabled int) bool {
	return a.UserDeleted || a.TokenDeleted || a.UserStatus != userEnabled || a.TokenStatus != tokenEnabled ||
		(a.TokenExpires != -1 && a.TokenExpires < now)
}
func CanMarkSubmissionUnknown(job Job, runner string, now int64) bool {
	return job.SubmitState == "preparing" && job.UpstreamID == "" && job.LeaseOwner == runner && job.LeaseUntil > now
}
func ValidateItemSettlement(job Job, mode, purchase string, quota int) error {
	if job.FinancialDetails != "" || (mode == BatchSettlementOpenRouterJobCostV2 && !(job.UpstreamID == "" &&
		job.SubmitState == "rejected" && quota == 0 && purchase == "unbilled")) {
		return ErrBatchConflict
	}
	return nil
}
func ValidateItemCharge(job Job, item Item, a Accounts, channelUsed int64, quota int) error {
	if int64(a.UserQuota) < math.MinInt32+int64(quota) || int64(a.UserUsed) > math.MaxInt32-int64(quota) ||
		a.UserRequests >= math.MaxInt32 || a.TokenQuota < math.MinInt+quota || a.TokenUsed > math.MaxInt-quota ||
		channelUsed > math.MaxInt64-int64(quota) || job.ChargedQuota > math.MaxInt64-int64(quota) ||
		job.SettledCount >= math.MaxInt {
		return errors.New("batch settlement arithmetic overflow")
	}
	if a.UserHeld < item.ReservedQuota || a.TokenHeld < item.ReservedQuota {
		return ErrBatchConflict
	}
	return nil
}
func ValidateAggregateSnapshot(mode string, basis *billing.ProviderBasis) error {
	if mode != BatchSettlementOpenRouterJobCostV2 || basis == nil || basis.Version != BatchSettlementOpenRouterJobCostV2 {
		return ErrBatchConflict
	}
	return nil
}
func AggregateDispatchReady(job Job) (bool, error) {
	if !State(job.Status).Terminal() {
		return false, nil
	}
	if job.UpstreamID == "" || (job.SubmitState != "submitted" && job.SubmitState != "cancel_requested" && job.SubmitState != "cancel_sent") {
		return false, ErrBatchConflict
	}
	return true, nil
}

type AggregateMath interface {
	Cost([]byte) (float64, error)
	Provider(billing.ProviderBasis, float64) (int, *billing.Clamp, error)
}

func AggregateQuota(math AggregateMath, basis billing.ProviderBasis, raw []byte) (float64, int, error) {
	cost, err := math.Cost(raw)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrBatchConflict, err)
	}
	quota, clamp, err := math.Provider(basis, cost)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrBatchConflict, err)
	}
	if clamp != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrBatchConflict, clamp)
	}
	return cost, quota, nil
}
func ValidateAggregateCharge(job Job, a Accounts, channelUsed int64, quota int) error {
	if int64(a.UserQuota) < math.MinInt32+int64(quota) || int64(a.UserUsed) > math.MaxInt32-int64(quota) ||
		job.RequestCount > math.MaxInt32 || a.UserRequests > math.MaxInt32-job.RequestCount ||
		a.TokenQuota < math.MinInt+quota || a.TokenUsed > math.MaxInt-quota || channelUsed > math.MaxInt64-int64(quota) {
		return fmt.Errorf("%w: batch settlement arithmetic overflow", ErrBatchConflict)
	}
	if job.ReservedQuota < 0 || a.UserHeld < job.ReservedQuota || a.TokenHeld < job.ReservedQuota {
		return ErrBatchConflict
	}
	return nil
}

func ValidateReservationInput(count int, total, maxWallet int64) error {
	if count == 0 || total < 0 || total > maxWallet {
		return errors.New("invalid batch reservation")
	}
	return nil
}
func ValidateReservationAccounts(job Job, a Accounts, unlimited bool, itemQuotas []int64, maxWallet, maxQuota int64) error {
	if a.UserHeld < 0 || a.UserHeld > maxWallet-job.ReservedQuota || int64(a.UserQuota)-a.UserHeld < job.ReservedQuota {
		return ErrBatchInsufficientQuota
	}
	if a.TokenHeld < 0 || a.TokenHeld > maxWallet-job.ReservedQuota || (!unlimited && int64(a.TokenQuota)-a.TokenHeld < job.ReservedQuota) {
		return ErrBatchInsufficientQuota
	}
	var sum int64
	for _, quota := range itemQuotas {
		if quota < 0 || quota > maxQuota || sum > job.ReservedQuota-quota {
			return errors.New("invalid item reservation")
		}
		sum += quota
	}
	if sum != job.ReservedQuota {
		return errors.New("batch reservation does not equal its item reservations")
	}
	return nil
}

package reset_subscriptions

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
)

// Store resets all selected rows in one transaction, preserving the cached plan read.
type Store interface {
	Reset(run func(domain.ResetTx) error) error
}
type Input struct {
	UserID, PlanID   int
	AdvanceResetTime bool
}
type Usecase struct {
	store Store
	now   func() int64
}

func NewUsecase(store Store, now func() int64) Usecase { return Usecase{store: store, now: now} }
func (u Usecase) Execute(in Input) (domain.ResetResult, error) {
	if in.PlanID <= 0 {
		return domain.ResetResult{}, errors.New("invalid planId")
	}
	now := u.now()
	var result domain.ResetResult
	err := u.store.Reset(func(tx domain.ResetTx) error {
		plan, err := tx.PlanByID(in.PlanID)
		if err != nil {
			return err
		}
		rows, err := tx.ResetCandidates(in.UserID, plan.Id, now)
		if err != nil {
			return err
		}
		if len(rows) == 0 && in.UserID > 0 {
			return domain.ErrNoActivePlan
		}
		for i := range rows {
			if err := domain.ResetSubscription(&rows[i], plan, now, in.AdvanceResetTime); err != nil {
				return err
			}
			if err := tx.SaveSubscription(&rows[i]); err != nil {
				return err
			}
		}
		users := make([]int, 0, len(rows))
		seen := make(map[int]struct{}, len(rows))
		for _, row := range rows {
			if _, ok := seen[row.UserId]; !ok {
				seen[row.UserId] = struct{}{}
				users = append(users, row.UserId)
			}
		}
		result = domain.ResetResult{PlanId: plan.Id, MatchedCount: len(rows), ResetCount: len(rows), UserCount: len(users), AdvanceResetTime: in.AdvanceResetTime, PlanTitle: plan.Title, AffectedUserIds: users}
		return nil
	})
	if err != nil {
		return domain.ResetResult{}, err
	}
	return result, nil
}

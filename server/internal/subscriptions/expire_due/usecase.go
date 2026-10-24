package expire_due

import (
	"github.com/QuantumNous/new-api/internal/subscriptions/domain"
	"strings"
)

type Store interface {
	ExpiredCandidates(limit int, now int64) ([]domain.UserSubscription, error)
	ExpireUser(func(domain.ExpiryTx) error) error
}
type Usecase struct {
	store   Store
	now     func() int64
	refresh func(int, string)
}

func NewUsecase(store Store, now func() int64, refresh func(int, string)) Usecase {
	return Usecase{store, now, refresh}
}
func (u Usecase) Execute(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := u.now()
	subs, err := u.store.ExpiredCandidates(limit, now)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}
	count := 0
	users := make(map[int]struct{}, len(subs))
	for _, sub := range subs {
		if sub.UserId > 0 {
			users[sub.UserId] = struct{}{}
		}
	}
	for id := range users {
		cacheGroup := ""
		err := u.store.ExpireUser(func(tx domain.ExpiryTx) error {
			n, err := tx.ExpireUser(id, now)
			if err != nil {
				return err
			}
			count += n
			active, err := tx.ActiveUpgrade(id, now, 0)
			if err == nil && active {
				return nil
			}
			last, err := tx.LastExpiredTransition(id)
			if err != nil || last == nil {
				return nil
			}
			current, err := tx.UserGroup(id)
			if err != nil {
				return err
			}
			target := strings.TrimSpace(last.DowngradeGroup)
			if target == "" {
				upgrade := strings.TrimSpace(last.UpgradeGroup)
				prev := strings.TrimSpace(last.PrevUserGroup)
				if upgrade == "" || prev == "" || current != upgrade {
					return nil
				}
				target = prev
			}
			if target == "" || target == current {
				return nil
			}
			if err := tx.SetUserGroup(id, target); err != nil {
				return err
			}
			cacheGroup = target
			return nil
		})
		if err != nil {
			return count, err
		}
		if cacheGroup != "" {
			u.refresh(id, "subscription expiration")
		}
	}
	return count, nil
}

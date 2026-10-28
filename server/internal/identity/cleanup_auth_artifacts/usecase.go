package cleanup_auth_artifacts

import (
	"errors"
	"fmt"
	"time"
)

type Store interface {
	CountIssued(int64) (int64, error)
	DeleteExpiredSessions(int64) error
	DeleteRevokedSessions(int64) error
	DeleteExpiredFlows(time.Time) error
}

type Usecase struct {
	store     Store
	threshold func() int
	report    func(string)
}

func NewUsecase(store Store, threshold func() int, report func(string)) Usecase {
	return Usecase{store: store, threshold: threshold, report: report}
}

// Execute keeps cleanup independent: an earlier failure never suppresses a later deletion.
func (u Usecase) Execute() error {
	now := time.Now()
	var failures []error
	count, err := u.store.CountIssued(now.Add(-time.Hour).Unix())
	if err != nil {
		u.report("failed to count hourly user session issuance: " + err.Error())
		failures = append(failures, err)
	} else if count > int64(u.threshold()) {
		u.report(fmt.Sprintf("hourly user session issuance exceeded alert threshold: count=%d threshold=%d window_seconds=%d", count, u.threshold(), int64(time.Hour/time.Second)))
	}
	if err := u.store.DeleteExpiredSessions(now.Unix()); err != nil {
		u.report("failed to delete expired user sessions: " + err.Error())
		failures = append(failures, err)
	}
	if err := u.store.DeleteRevokedSessions(now.Unix()); err != nil {
		u.report("failed to delete old revoked user sessions: " + err.Error())
		failures = append(failures, err)
	}
	if err := u.store.DeleteExpiredFlows(now); err != nil {
		u.report("failed to delete expired authentication flows: " + err.Error())
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

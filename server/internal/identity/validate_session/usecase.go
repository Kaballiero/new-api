package validate_session

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"time"
)

type ValidateOperation interface {
	LoadSession(sid string) (domain.SessionState, error)
	LoadAccount(userID int) (domain.AccountState, error)
}

func (usecase Usecase) Execute(userID int, sid string, authVersion int64, sessionVersion int64) error {
	session, err := usecase.operation.LoadSession(sid)
	if err != nil {
		if errors.Is(err, usecase.policy.Inactive()) {
			return usecase.policy.Revoked()
		}
		return err
	}
	now := time.Now().Unix()
	if session.UserID != userID || session.Status != usecase.policy.ActiveStatus || session.RevokedAt != 0 || session.ExpiresAt <= now || session.Version != sessionVersion || session.UserAuthVersion != authVersion {
		return usecase.policy.Revoked()
	}
	user, err := usecase.operation.LoadAccount(userID)
	if err != nil {
		return err
	}
	if user.Status != usecase.policy.EnabledStatus || user.AuthVersion != authVersion {
		return usecase.policy.Revoked()
	}
	return nil
}

type Usecase struct {
	operation ValidateOperation
	policy    domain.SessionPolicy
}

func NewUsecase(operation ValidateOperation, policy domain.SessionPolicy) Usecase {
	return Usecase{operation: operation, policy: policy}
}

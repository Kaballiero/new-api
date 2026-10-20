package validate_session_reference

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"strings"
)

type Operation interface {
	InvalidError() error
	LoadSession(sid string) (authVersion, sessionVersion int64, err error)
	Validate(domain.AuthIdentity) error
}

func (usecase Usecase) Execute(userID int, sid string) (domain.AuthIdentity, error) {
	if userID <= 0 || strings.TrimSpace(sid) == "" {
		return domain.AuthIdentity{}, usecase.operation.InvalidError()
	}
	authVersion, sessionVersion, err := usecase.operation.LoadSession(sid)
	if err != nil {
		return domain.AuthIdentity{}, err
	}
	identity := domain.AuthIdentity{UserID: userID, SessionID: sid, UserAuthVersion: authVersion, SessionVersion: sessionVersion}
	if err := usecase.operation.Validate(identity); err != nil {
		return domain.AuthIdentity{}, err
	}
	return identity, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}

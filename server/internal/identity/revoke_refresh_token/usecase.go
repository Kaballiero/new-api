package revoke_refresh_token

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type RevokeOperation interface {
	RevokeByHash(sid, secret, reason string) error
}

func (usecase Usecase) Execute(rawToken string, expectedSID string, reason string) error {
	sid, secret, ok := usecase.split(rawToken)
	if !ok {
		return nil
	}
	if expectedSID = strings.TrimSpace(expectedSID); expectedSID != "" && expectedSID != sid {
		return domain.ErrLoginSessionMismatch
	}
	return usecase.operation.RevokeByHash(sid, secret, reason)
}

type Usecase struct {
	operation RevokeOperation
	split     func(string) (
		string, string, bool)
}

func NewUsecase(operation RevokeOperation, split func(string) (string, string, bool)) Usecase {
	return Usecase{operation: operation, split: split}
}

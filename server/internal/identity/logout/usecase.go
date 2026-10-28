package logout

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Operation interface {
	RefreshSID(string) (string, bool)
	ParseAccess(string) (domain.AuthIdentity, error)
	Revoke(userID int, sid, reason string) (bool, error)
	RevokeRefresh(raw, expectedSID, reason string) error
}

func (usecase Usecase) Execute(input Input) (Result, error) {
	expected := strings.TrimSpace(input.ExpectedSID)
	cookieSID, hasSID := usecase.operation.RefreshSID(input.RefreshToken)
	if expected != "" && input.CookiePresent && hasSID && cookieSID != expected {
		return Result{}, domain.ErrLoginSessionMismatch
	}
	if input.AccessToken != "" {
		if identity, err := usecase.operation.ParseAccess(input.AccessToken); err == nil {
			if expected != "" && expected != identity.SessionID {
				return Result{}, domain.ErrLoginSessionMismatch
			}
			if _, err := usecase.operation.Revoke(identity.UserID, identity.SessionID, "logout"); err != nil {
				return Result{}, err
			}
			cleared := false
			if input.CookiePresent && hasSID && cookieSID == identity.SessionID {
				if err := usecase.operation.RevokeRefresh(input.RefreshToken, identity.SessionID, "logout"); err != nil {
					return Result{}, err
				}
				cleared = true
			}
			return Result{RevokedSID: identity.SessionID, ClearCookie: cleared, Detailed: true}, nil
		}
	}
	if !input.CookiePresent || input.RefreshToken == "" {
		return Result{ClearCookie: true}, nil
	}
	if err := usecase.operation.RevokeRefresh(input.RefreshToken, expected, "logout"); err != nil {
		return Result{}, err
	}
	return Result{ClearCookie: true}, nil
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}

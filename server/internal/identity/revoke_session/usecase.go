package revoke_session

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Revoker interface {
	Revoke(userID int, sid, reason string) (bool, error)
}

// Execute revokes a session of the user and returns its trimmed id. An empty id
// is domain.ErrSessionIDRequired, a session that was not revoked is
// domain.ErrSessionNotFound.
func (usecase Usecase) Execute(input Input) (string, error) {
	sid := strings.TrimSpace(input.SID)
	if sid == "" {
		return "", domain.ErrSessionIDRequired
	}
	revoked, err := usecase.revoker.Revoke(input.UserID, sid, "user_revoked")
	if err != nil {
		return "", err
	}
	if !revoked {
		return "", domain.ErrSessionNotFound
	}
	return sid, nil
}

type Usecase struct {
	revoker Revoker
}

func NewUsecase(revoker Revoker) Usecase {
	return Usecase{revoker: revoker}
}

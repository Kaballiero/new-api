package revoke_session

import "strings"

type Revoker interface {
	Revoke(userID int, sid, reason string) (bool, error)
}

func (usecase Usecase) Execute(input Input) (Output, error) {
	sid := strings.TrimSpace(input.SID)
	if sid == "" {
		return Output{Invalid: true}, nil
	}
	revoked, err := usecase.revoker.Revoke(input.UserID, sid, "user_revoked")
	return Output{SID: sid, Revoked: revoked}, err
}

type Usecase struct {
	revoker Revoker
}

func NewUsecase(revoker Revoker) Usecase {
	return Usecase{revoker: revoker}
}

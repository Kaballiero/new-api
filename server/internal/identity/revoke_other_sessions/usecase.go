package revoke_other_sessions

type Revoker interface {
	RevokeOthers(userID int, currentSID, reason string) (int64, error)
}

func (usecase Usecase) Execute(input Input) (Output, error) {
	count, err := usecase.revoker.RevokeOthers(input.UserID, input.CurrentSID, "user_revoked_others")
	return Output{Count: count}, err
}

type Usecase struct {
	revoker Revoker
}

func NewUsecase(revoker Revoker) Usecase {
	return Usecase{revoker: revoker}
}

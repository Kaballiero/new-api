package list_sessions

type Reader interface {
	List(userID int, currentSID string) (Output, error)
}

func (usecase Usecase) Execute(input Input) (Output, error) {
	return usecase.reader.List(input.UserID, input.CurrentSID)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

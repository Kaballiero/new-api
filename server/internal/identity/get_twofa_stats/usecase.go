package get_twofa_stats

type Reader interface{ Stats() (Output, error) }

func (usecase Usecase) Execute() (Output, error) { return usecase.reader.Stats() }

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

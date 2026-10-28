package get_key_groups

type Reader interface {
	Group() (string, error)
	Options(string) []string
	Max() int
}

func (usecase Usecase) Execute() (Output, error) {
	g, e := usecase.reader.Group()
	if e != nil {
		return Output{}, e
	}
	return Output{usecase.reader.Options(g), usecase.reader.Max()}, nil
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

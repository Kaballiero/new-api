package generate_token

type Store interface {
	CountToken(string) (int64, error)
	Update(int, string) error
}
type Usecase struct {
	store       Store
	generate    func() (string, error)
	fingerprint func(string) string
	audit       func(int, string)
}

func NewUsecase(store Store, generate func() (string, error), fingerprint func(string) string, audit func(int, string)) Usecase {
	return Usecase{store: store, generate: generate, fingerprint: fingerprint, audit: audit}
}
func (usecase Usecase) Execute(input Input) (Output, error) {
	key, err := usecase.generate()
	if err != nil {
		return Output{}, err
	}
	existing, err := usecase.store.CountToken(key)
	if err != nil {
		return Output{}, err
	}
	if existing != 0 {
		return Output{Duplicate: true}, nil
	}
	if err := usecase.store.Update(input.UserID, key); err != nil {
		return Output{}, err
	}
	usecase.audit(input.UserID, usecase.fingerprint(key))
	return Output{Token: key}, nil
}

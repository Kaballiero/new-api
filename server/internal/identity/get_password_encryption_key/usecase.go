package get_password_encryption_key

type Keys interface {
	Enabled() bool
	PublicKey() (string, string)
}

func (usecase Usecase) Execute() Output {
	if !usecase.keys.Enabled() {
		return Output{}
	}
	id, key := usecase.keys.PublicKey()
	if id == "" || key == "" {
		return Output{Unavailable: true}
	}
	return Output{Enabled: true, KeyID: id, PublicKey: key}
}

type Usecase struct {
	keys Keys
}

func NewUsecase(keys Keys) Usecase {
	return Usecase{keys: keys}
}

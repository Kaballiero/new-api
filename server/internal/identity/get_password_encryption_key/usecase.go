package get_password_encryption_key

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Keys interface {
	Enabled() bool
	PublicKey() (string, string)
}

// Execute returns the public key of encrypted password login, or
// domain.ErrPasswordKeyMissing when encryption is enabled without a key.
func (usecase Usecase) Execute() (Output, error) {
	if !usecase.keys.Enabled() {
		return Output{}, nil
	}
	id, key := usecase.keys.PublicKey()
	if id == "" || key == "" {
		return Output{}, domain.ErrPasswordKeyMissing
	}
	return Output{Enabled: true, KeyID: id, PublicKey: key}, nil
}

type Usecase struct {
	keys Keys
}

func NewUsecase(keys Keys) Usecase {
	return Usecase{keys: keys}
}

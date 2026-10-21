package revoke_token

import "github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

type Transaction interface {
	LockToken(int) (string, error)
	Clear(int) error
}
type Store interface {
	Transaction(func(Transaction) error) error
}
type Usecase struct {
	store       Store
	fingerprint func(string) string
	audit       func(string)
}

// audit may be nil when the caller records no audit event (no HTTP audit context).
func NewUsecase(store Store, fingerprint func(string) string, audit func(string)) Usecase {
	return Usecase{store: store, fingerprint: fingerprint, audit: audit}
}
func (usecase Usecase) Execute(input Input) (Output, error) {
	var tokenRef string
	err := usecase.store.Transaction(func(tx Transaction) error {
		token, err := tx.LockToken(input.UserID)
		if err != nil {
			return err
		}
		tokenRef = usecase.fingerprint(token)
		if tokenRef == "" {
			return nil
		}
		return tx.Clear(input.UserID)
	})
	if err != nil {
		// The attempted generation is still reported so callers can correlate it.
		return Output{TokenRef: tokenRef}, domain.Internal(err)
	}
	if tokenRef != "" && usecase.audit != nil {
		usecase.audit(tokenRef)
	}
	return Output{TokenRef: tokenRef}, nil
}

package revoke_token

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

// audit is nil for the pre-existing model bridge, which has no HTTP audit context.
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
	if err == nil && tokenRef != "" && usecase.audit != nil {
		usecase.audit(tokenRef)
	}
	return Output{TokenRef: tokenRef}, err
}

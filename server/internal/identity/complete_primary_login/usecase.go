package complete_primary_login

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	PrimaryValid() bool
	LoadProfile() error
	CreateSession(expectedVersion int64) error
}

// Execute returns domain.ErrAccountBanned for a disabled account, a
// domain.Internal profile failure or the session creation error.
func (usecase Usecase) Execute(operation Operation, expectedVersion int64) error {
	if !operation.PrimaryValid() {
		return domain.ErrAccountBanned
	}
	if err := operation.LoadProfile(); err != nil {
		return domain.Internal(err)
	}
	return operation.CreateSession(expectedVersion)
}

type Usecase struct{}

func NewUsecase() Usecase { return Usecase{} }

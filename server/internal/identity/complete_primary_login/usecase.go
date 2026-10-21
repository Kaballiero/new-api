package complete_primary_login

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Operation interface {
	PrimaryValid() bool
	LoadProfile() error
	CreateSession(expectedVersion int64) error
}

// Execute returns domain.ErrAccountBanned for a disabled account, a
// domain.Internal profile failure or the session creation error.
func (usecase Usecase) Execute(expectedVersion int64) error {
	if !usecase.operation.PrimaryValid() {
		return domain.ErrAccountBanned
	}
	if err := usecase.operation.LoadProfile(); err != nil {
		return domain.Internal(err)
	}
	return usecase.operation.CreateSession(expectedVersion)
}

type Usecase struct {
	operation Operation
}

func NewUsecase(operation Operation) Usecase {
	return Usecase{operation: operation}
}

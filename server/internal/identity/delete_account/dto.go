package delete_account

import "github.com/QuantumNous/new-api/internal/identity/domain"

type Input struct{ Identity domain.AuthIdentity }
type Result struct {
	RootDenied bool
	Cause      error
}

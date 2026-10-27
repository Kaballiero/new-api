package accountsidentity

import (
	"errors"
	accountdomain "github.com/QuantumNous/new-api/internal/accounts/domain"

	"github.com/QuantumNous/new-api/internal/identity/delete_account"
	domain "github.com/QuantumNous/new-api/internal/identity/domain"
)

type Deletion struct {
	Remove func(domain.AuthIdentity) error
}

func (op Deletion) Delete(input delete_account.Input) error {
	return op.Remove(input.Identity)
}
func (Deletion) RootDenied(err error) bool {
	return errors.Is(err, accountdomain.ErrCannotDeleteRootUser)
}

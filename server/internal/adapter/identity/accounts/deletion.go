package accountsidentity

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/identity/delete_account"
	"github.com/QuantumNous/new-api/model"
)

type Deletion struct{}

func (Deletion) Delete(input delete_account.Input) error {
	return model.DeleteUserForSession(input.Identity)
}
func (Deletion) RootDenied(err error) bool { return errors.Is(err, model.ErrCannotDeleteRootUser) }

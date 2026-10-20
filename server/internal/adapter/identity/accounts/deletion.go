package accountsidentity

import (
	"github.com/QuantumNous/new-api/internal/identity/delete_account"
	"github.com/QuantumNous/new-api/model"
)

type Deletion struct{}

func (Deletion) Delete(input delete_account.Input) error {
	return model.DeleteUserForSession(input.Identity)
}
func (Deletion) RootError() error { return model.ErrCannotDeleteRootUser }

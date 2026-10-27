package identitycore

import (
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	passwordadapter "github.com/QuantumNous/new-api/internal/adapter/identity/password"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	"github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/change_password"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// ChangeUserPassword preserves the same session recheck and credential transaction.
func ChangeUserPassword(identity domain.AuthIdentity, update *accountrecord.User, firstPassword bool) error {
	op := &passwordadapter.PasswordWriteOperation{DB: func() *gorm.DB { return model.DB }, Validate: identitystorage.SQL().ValidateAuthSessionWithTx, Increment: accountstorage.Cache().IncrementUserAuthVersionWithTx, Identity: identity, Update: update}
	return change_password.NewUsecase(op).Execute(firstPassword)
}

package identitycore

import (
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	emailadapter "github.com/QuantumNous/new-api/internal/adapter/identity/emailbinding"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/confirm_email_binding"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
)

func StartEmailBinding(identity domain.AuthIdentity, authorization *domain.FlowAuthorization, email string) (*identitydto.EmailBindingData, error) {
	op := &emailadapter.Operation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Dependencies: identityRuntime().EmailDependencies, Identity: identity, Authorization: authorization, Email: email}
	if err := identityRuntime().StartEmailBinding.Execute(op); err != nil {
		return nil, err
	}
	return op.Result, nil
}

func ResendAccountEmailBinding(identity domain.AuthIdentity, token string) (*identitydto.EmailBindingData, error) {
	op := &emailadapter.Operation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Dependencies: identityRuntime().EmailDependencies, Identity: identity, Token: token}
	if err := identityRuntime().ResendEmailBinding.Execute(op); err != nil {
		return nil, err
	}
	return op.Result, nil
}

func FinishEmailBinding(identity domain.AuthIdentity, token, newCode, oldCode string) (*identitystore.EmailBindingState, error) {
	op := &emailadapter.Operation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), Dependencies: identityRuntime().EmailDependencies, Identity: identity, Token: token, NewCode: newCode, OldCode: oldCode}
	if err := confirm_email_binding.Finish(op); err != nil {
		return nil, err
	}
	return op.State, nil
}

func emailBindingDependencies() emailadapter.Dependencies {
	return emailadapter.Dependencies{ValidateEmailFunc: emailadapter.ValidateAccountEmail, ValidateAuthorizationFunc: ValidateFlowAuthorization, GenerateCodesFunc: emailadapter.GenerateEmailBindingCodes, DeliverFunc: emailadapter.SendEmailBindingCodes, DataFunc: emailadapter.BindingData, NotifyFunc: emailadapter.NotifyAccountSecurityChange}
}

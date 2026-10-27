package identitycore

import (
	"github.com/QuantumNous/new-api/internal/adapter/identity/factors"
	accountstorage "github.com/QuantumNous/new-api/internal/app/accountstorage"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"
)

func StartTwoFASetup(identity domain.AuthIdentity, authorization *domain.FlowAuthorization) (*identitydto.TwoFASetup, error) {
	op := &factors.StartTwoFASetupOperation{Accounts: accountstorage.SQL(), SQL: identitystorage.SQL(), ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, Authorization: authorization}
	if err := identityRuntime().StartTwofaSetup.Execute(op); err != nil {
		return nil, err
	}
	return op.Setup, nil
}

func FinishTwoFASetup(identity domain.AuthIdentity, flowToken, code string) error {
	return finish_twofa_setup.FinishTwoFASetup(&factors.FinishTwoFASetupOperation{SQL: identitystorage.SQL(), ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, FlowToken: flowToken, Code: code})
}

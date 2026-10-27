package identitycore

import (
	"github.com/QuantumNous/new-api/internal/adapter/identity/factors"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"
	"github.com/QuantumNous/new-api/model"
)

func StartTwoFASetup(identity domain.AuthIdentity, authorization *model.AuthFlowAuthorization) (*identitydto.TwoFASetup, error) {
	op := &factors.StartTwoFASetupOperation{ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, Authorization: authorization}
	if err := identityRuntime().StartTwofaSetup.Execute(op); err != nil {
		return nil, err
	}
	return op.Setup, nil
}

func FinishTwoFASetup(identity domain.AuthIdentity, flowToken, code string) error {
	return finish_twofa_setup.FinishTwoFASetup(&factors.FinishTwoFASetupOperation{ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, FlowToken: flowToken, Code: code})
}

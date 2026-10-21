package service

import (
	"github.com/QuantumNous/new-api/internal/adapter/identity/factors"
	identitydto "github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/finish_twofa_setup"
	"github.com/QuantumNous/new-api/internal/identity/start_twofa_setup"

	"github.com/QuantumNous/new-api/model"
)

type TwoFASetup = identitydto.TwoFASetup

func StartTwoFASetup(identity AuthIdentity, authorization *model.AuthFlowAuthorization) (*TwoFASetup, error) {
	op := &factors.StartTwoFASetupOperation{ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, Authorization: authorization}
	if err := start_twofa_setup.NewUsecase(op).Execute(); err != nil {
		return nil, err
	}
	return op.Setup, nil
}

func FinishTwoFASetup(identity AuthIdentity, flowToken, code string) error {
	return finish_twofa_setup.FinishTwoFASetup(&factors.FinishTwoFASetupOperation{ValidateAuthorizationFunc: ValidateFlowAuthorization, Identity: identity, FlowToken: flowToken, Code: code})
}

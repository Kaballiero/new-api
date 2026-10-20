package factors

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/pquerna/otp"
	"time"
)

type StartTwoFASetupOperation struct {
	ValidateAuthorizationFunc func(domain.AuthIdentity, domain.VerificationOperation, *model.AuthFlowAuthorization) error
	Identity                  domain.AuthIdentity
	Authorization             *model.AuthFlowAuthorization
	username                  string
	key                       *otp.Key
	codes                     []string
	expiresAt                 time.Time
	FlowToken                 string
	Setup                     *dto.TwoFASetup
}

func (o *StartTwoFASetupOperation) ValidateAuthorization() error {
	return o.ValidateAuthorizationFunc(o.Identity, domain.VerificationOperation{Scope: "2fa.setup"}, o.Authorization)
}
func (o *StartTwoFASetupOperation) LoadAccountName() (string, error) {
	user, err := model.GetUserById(o.Identity.UserID, false)
	if err != nil {
		return "", err
	}
	o.username = user.Username
	return o.username, nil
}
func (o *StartTwoFASetupOperation) GenerateSecret(username string) error {
	var err error
	o.key, err = common.GenerateTOTPSecret(username)
	return err
}
func (o *StartTwoFASetupOperation) GenerateBackupCodes() error {
	var err error
	o.codes, err = common.GenerateBackupCodes()
	return err
}
func (o *StartTwoFASetupOperation) CreateEnrollment() error {
	o.expiresAt = time.Now().Add(5 * time.Minute)
	var err error
	o.FlowToken, err = model.CreateTwoFAEnrollment(o.Identity, o.Authorization, o.key.Secret(), o.codes, o.expiresAt)
	return err
}
func (o *StartTwoFASetupOperation) CaptureSetup() {
	o.Setup = &dto.TwoFASetup{Secret: o.key.Secret(), QRCodeData: common.GenerateQRCodeData(o.key.Secret(), o.username), BackupCodes: o.codes, FlowToken: o.FlowToken, ExpiresAt: o.expiresAt.Unix()}
}

type FinishTwoFASetupOperation struct {
	ValidateAuthorizationFunc func(domain.AuthIdentity, domain.VerificationOperation, *model.AuthFlowAuthorization) error
	Identity                  domain.AuthIdentity
	FlowToken, Code           string
	flow                      *model.AuthFlow
	payload                   struct {
		Authorization *model.AuthFlowAuthorization `json:"authorization"`
	}
}

func (o *FinishTwoFASetupOperation) LoadSetupFlow() error {
	var err error
	o.flow, err = model.GetAuthFlow(o.FlowToken, model.AuthFlowMatch{
		Purpose: model.AuthFlowPurposeTwoFASetup, UserId: o.Identity.UserID, SessionId: o.Identity.SessionID,
	})
	return err
}
func (o *FinishTwoFASetupOperation) DecodeAuthorization() error {
	return common.UnmarshalJsonStr(o.flow.Payload, &o.payload)
}
func (o *FinishTwoFASetupOperation) ValidateAuthorization() error {
	return o.ValidateAuthorizationFunc(o.Identity, domain.VerificationOperation{Scope: "2fa.setup"}, o.payload.Authorization)
}
func (o *FinishTwoFASetupOperation) EnableEnrollment() error {
	return model.EnableTwoFAEnrollment(o.Identity, o.FlowToken, o.Code)
}

package factors

import (
	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"

	"github.com/pquerna/otp"
	"time"
)

type StartTwoFASetupOperation struct {
	Accounts accountstore.Store

	SQL identitystore.Store

	ValidateAuthorizationFunc func(domain.AuthIdentity, domain.VerificationOperation, *domain.FlowAuthorization) error
	Identity                  domain.AuthIdentity
	Authorization             *domain.FlowAuthorization
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
	user, err := o.Accounts.GetUserById(o.Identity.UserID, false)
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
	o.FlowToken, err = o.SQL.CreateTwoFAEnrollment(o.Identity, o.Authorization, o.key.Secret(), o.codes, o.expiresAt)
	return err
}
func (o *StartTwoFASetupOperation) CaptureSetup() {
	o.Setup = &dto.TwoFASetup{Secret: o.key.Secret(), QRCodeData: common.GenerateQRCodeData(o.key.Secret(), o.username), BackupCodes: o.codes, FlowToken: o.FlowToken, ExpiresAt: o.expiresAt.Unix()}
}

type FinishTwoFASetupOperation struct {
	SQL identitystore.Store

	ValidateAuthorizationFunc func(domain.AuthIdentity, domain.VerificationOperation, *domain.FlowAuthorization) error
	Identity                  domain.AuthIdentity
	FlowToken, Code           string
	flow                      *identityrecord.AuthFlow
	payload                   struct {
		Authorization *domain.FlowAuthorization `json:"authorization"`
	}
}

func (o *FinishTwoFASetupOperation) LoadSetupFlow() error {
	var err error
	o.flow, err = o.SQL.GetAuthFlow(o.FlowToken, identitystore.AuthFlowMatch{
		Purpose: domain.AuthFlowPurposeTwoFASetup, UserId: o.Identity.UserID, SessionId: o.Identity.SessionID,
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
	return o.SQL.EnableTwoFAEnrollment(o.Identity, o.FlowToken, o.Code)
}

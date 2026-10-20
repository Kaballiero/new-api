package verification

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type PolicyOperation struct {
	Methods []dto.VerificationMethodOption
}

func (op *PolicyOperation) PasskeyEnabled() bool {
	return system_setting.GetPasskeySettings().Enabled
}

func (op *PolicyOperation) AddMethod(method string, available bool, reason string) {
	op.Methods = append(op.Methods, dto.VerificationMethodOption{Method: method, Available: available, Reason: reason})
}

type MethodOptions []dto.VerificationMethodOption

func (op MethodOptions) MethodCount() int { return len(op) }

func (op MethodOptions) MethodOption(i int) (string, bool) {
	return op[i].Method, op[i].Available
}

type RequirementsOperation struct {
	PolicyFunc    func(string, model.UserVerificationState) ([]dto.VerificationMethodOption, error)
	ProvidersFunc func(*model.User) ([]get_verification_requirements.VerificationOAuthProvider, error)
	Identity      domain.AuthIdentity
	Scope         string
	state         *model.UserVerificationState
	Methods       []dto.VerificationMethodOption
	user          *model.User
	Requirements  *get_verification_requirements.VerificationRequirements
}

func (op *RequirementsOperation) LoadState() (int, int64, int, error) {
	var err error
	op.state, err = model.GetUserVerificationState(op.Identity.UserID)
	if err != nil {
		return 0, 0, 0, err
	}
	return op.state.Status, op.state.AuthVersion, op.state.Role, nil
}

func (op *RequirementsOperation) Policy() error {
	var err error
	op.Methods, err = op.PolicyFunc(op.Scope, *op.state)
	return err
}

func (op *RequirementsOperation) CaptureRequirements() {
	op.Requirements = &get_verification_requirements.VerificationRequirements{Scope: op.Scope, Methods: op.Methods, OAuthProviders: []get_verification_requirements.VerificationOAuthProvider{}, PasswordEncryptionEnabled: common.PasswordLoginEncryptionEnabled}
}

func (op *RequirementsOperation) MethodCount() int { return len(op.Methods) }

func (op *RequirementsOperation) MethodOption(i int) (string, bool) {
	return op.Methods[i].Method, op.Methods[i].Available
}

func (op *RequirementsOperation) PasswordLoginEnabled() bool {
	return common.PasswordLoginEnabled
}

func (op *RequirementsOperation) DisableMethod(i int, reason string) {
	op.Methods[i].Available, op.Methods[i].Reason = false, reason
}

func (op *RequirementsOperation) LoadOAuthAccount() error {
	var err error
	op.user, err = model.GetUserById(op.Identity.UserID, false)
	return err
}

func (op *RequirementsOperation) LoadOAuthProviders() (int, error) {
	var err error
	op.Requirements.OAuthProviders, err = op.ProvidersFunc(op.user)
	return len(op.Requirements.OAuthProviders), err
}

func (op *RequirementsOperation) HasTelegram() bool { return op.user.TelegramId != "" }

func (op *RequirementsOperation) TelegramConfigurationError() error {
	return oauth.TelegramConfigurationError()
}

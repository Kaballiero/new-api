package emailbinding

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
)

type Codes struct {
	New, Old         string
	NewHash, OldHash string
}
type Dependencies struct {
	ValidateEmailFunc         func(string) (string, error)
	ValidateAuthorizationFunc func(domain.AuthIdentity, domain.VerificationOperation, *domain.FlowAuthorization) error
	GenerateCodesFunc         func(bool) (Codes, error)
	DeliverFunc               func(identitystore.EmailBindingState, Codes) error
	DataFunc                  func(string, *identityrecord.AuthFlow, *identitystore.EmailBindingState) *dto.EmailBindingData
	NotifyFunc                func(string, string) error
}
type Operation struct {
	Accounts accountstore.Store

	SQL identitystore.Store

	Dependencies
	Identity                       domain.AuthIdentity
	Authorization                  *domain.FlowAuthorization
	Email, Token, NewCode, OldCode string
	State                          *identitystore.EmailBindingState
	Result                         *dto.EmailBindingData
	user                           *accountrecord.User
	flow                           *identityrecord.AuthFlow
	context                        []byte
	codes                          Codes
}

func (op *Operation) ValidateEmail() error {
	var err error
	op.Email, err = op.ValidateEmailFunc(op.Email)
	return err
}
func (op *Operation) BindOperation() error {
	email := op.Email
	if op.State != nil {
		email = op.State.Email
	}
	var err error
	op.context, err = common.Marshal(domain.AccountBindingContext{Provider: "email", Email: email})
	return err
}
func (op *Operation) ValidateAuthorization() error {
	authorization := op.Authorization
	if op.State != nil {
		authorization = op.State.Authorization
	}
	return op.ValidateAuthorizationFunc(op.Identity, domain.VerificationOperation{Scope: "account.binding.bind", Context: op.context}, authorization)
}
func (op *Operation) LoadAccount() error {
	var err error
	op.user, err = op.Accounts.GetUserById(op.Identity.UserID, false)
	return err
}
func (op *Operation) PrepareChange() {
	op.State = &identitystore.EmailBindingState{Authorization: op.Authorization, CurrentEmail: accountstore.NormalizeEmail(op.user.Email), Email: op.Email, ResendAt: time.Now().Add(identitystore.EmailBindingResendDelay).Unix()}
}
func (op *Operation) CurrentEmail() string        { return op.State.CurrentEmail }
func (op *Operation) AuthorizationMethod() string { return op.Authorization.Method }
func (op *Operation) GenerateCodes(requireOld bool) error {
	var err error
	op.codes, err = op.GenerateCodesFunc(requireOld)
	if err == nil {
		op.State.NewCodeHash, op.State.OldCodeHash = op.codes.NewHash, op.codes.OldHash
	}
	return err
}
func (op *Operation) CreateChange() error {
	var err error
	op.Token, op.flow, err = op.SQL.CreateEmailBinding(op.Identity, *op.State)
	return err
}
func (op *Operation) DeliverCodes() error { return op.DeliverFunc(*op.State, op.codes) }
func (op *Operation) InvalidateChange() {
	_, _ = op.SQL.ConsumeAuthFlow(op.Token, identitystore.AuthFlowMatch{Purpose: domain.AuthFlowPurposeEmailBinding, UserId: op.Identity.UserID, SessionId: op.Identity.SessionID})
}
func (op *Operation) CaptureData() { op.Result = op.DataFunc(op.Token, op.flow, op.State) }
func (op *Operation) NotifyCurrentEmail() bool {
	return op.NotifyFunc(op.State.CurrentEmail, "A change of your email address was requested") != nil
}
func (op *Operation) SetNotificationWarning(warning bool) { op.Result.NotificationWarning = warning }
func (op *Operation) LoadChange() error {
	var err error
	op.flow, op.State, err = op.SQL.GetEmailBinding(op.Identity, op.Token)
	return err
}
func (op *Operation) ResendAt() int64      { return op.State.ResendAt }
func (op *Operation) FailedAttempts() int  { return op.State.FailedAttempts }
func (op *Operation) MaximumAttempts() int { return identitystore.EmailBindingMaxAttempts }
func (op *Operation) HasOldCode() bool     { return op.State.OldCodeHash != "" }
func (op *Operation) ReplaceCodes() error {
	var err error
	op.flow, op.State, err = op.SQL.ResendEmailBinding(op.Identity, op.Token, op.codes.NewHash, op.codes.OldHash)
	return err
}
func (op *Operation) ValidateTargetEmail() error {
	_, err := op.ValidateEmailFunc(op.State.Email)
	return err
}
func (op *Operation) CompleteChange() error {
	var err error
	op.State, err = op.SQL.CompleteEmailBinding(op.Identity, op.Token, op.State.Email, op.NewCode, op.OldCode)
	return err
}

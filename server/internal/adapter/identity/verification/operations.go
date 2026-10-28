package verification

import (
	"crypto/hmac"

	"github.com/QuantumNous/new-api/common"
	accountstore "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/core"
	accountrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	identitystore "github.com/QuantumNous/new-api/internal/adapter/gorm/identity"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
	"github.com/QuantumNous/new-api/internal/identity/verify_security_input"

	"gorm.io/gorm"
)

type ProofClaims struct{ ID, Method string }
type Dependencies struct {
	SQL identitystore.Store

	ValidateSessionFunc func(domain.AuthIdentity) error
	RequireMethodFunc   func(domain.AuthIdentity, string, string) error
	IssueProofFunc      func(domain.AuthIdentity, string, domain.VerificationBinding) (string, int64, error)
	BindFunc            func(domain.VerificationOperation) (domain.VerificationBinding, error)
	VerifyClaimsFunc    func(string, domain.AuthIdentity, domain.VerificationBinding) (*ProofClaims, error)
	VerifyFactorFunc    func(*identityrecord.TwoFA, string) error
	CompleteFunc        func(domain.AuthIdentity, domain.VerificationBinding, string) (*dto.SecurityProof, error)
}
type CompleteSecurityOperation struct {
	Dependencies
	Identity domain.AuthIdentity
	Binding  domain.VerificationBinding
	Proof    *dto.SecurityProof
}

func (op *CompleteSecurityOperation) ValidateSession() error {
	return op.ValidateSessionFunc(op.Identity)
}

func (op *CompleteSecurityOperation) RequireMethod(method string) error {
	return op.RequireMethodFunc(op.Identity, op.Binding.Scope, method)
}

func (op *CompleteSecurityOperation) IssueProof(method string) error {
	token, expiresAt, err := op.IssueProofFunc(op.Identity, method, op.Binding)
	if err != nil {
		return err
	}
	op.Proof = &dto.SecurityProof{ProofToken: token, ExpiresAt: expiresAt, Method: method, Scope: op.Binding.Scope}
	return nil
}

type ConsumeProofOperation struct {
	Dependencies
	proofConsumption
	Raw           string
	Identity      domain.AuthIdentity
	Operation     domain.VerificationOperation
	Binding       domain.VerificationBinding
	claims        *ProofClaims
	Authorization *domain.FlowAuthorization
}

func (op *ConsumeProofOperation) BindOperation() error {
	var err error
	op.Binding, err = op.BindFunc(op.Operation)
	return err
}

func (op *ConsumeProofOperation) VerifyClaims() (string, error) {
	var err error
	op.claims, err = op.VerifyClaimsFunc(op.Raw, op.Identity, op.Binding)
	if err != nil {
		return "", err
	}
	op.proofConsumption.Token = op.claims.ID
	return op.claims.Method, nil
}

func (op *ConsumeProofOperation) ValidateSession() error {
	return op.ValidateSessionFunc(op.Identity)
}

func (op *ConsumeProofOperation) RequireMethod(method string) error {
	return op.RequireMethodFunc(op.Identity, op.Binding.Scope, method)
}

func (op *ConsumeProofOperation) CaptureAuthorization() {
	op.Authorization = &domain.FlowAuthorization{AuthSessionIdentity: op.Identity, ProofID: op.Flow.Id, Scope: op.Binding.Scope, ContextHash: op.Binding.ContextHash, Method: op.claims.Method}
}

type FlowAuthorizationOperation struct {
	Dependencies
	Identity      domain.AuthIdentity
	Operation     domain.VerificationOperation
	Authorization *domain.FlowAuthorization
	Binding       domain.VerificationBinding
}

func (op *FlowAuthorizationOperation) AuthorizationValid() bool {
	return op.Authorization != nil && op.Authorization.ProofID > 0 && op.Authorization.AuthSessionIdentity == op.Identity
}

func (op *FlowAuthorizationOperation) BindOperation() error {
	var err error
	op.Binding, err = op.BindFunc(op.Operation)
	return err
}

func (op *FlowAuthorizationOperation) BindingMatches() bool {
	return op.Authorization.Scope == op.Binding.Scope && hmac.Equal([]byte(op.Authorization.ContextHash), []byte(op.Binding.ContextHash))
}

func (op *FlowAuthorizationOperation) ValidateSession() error {
	return op.ValidateSessionFunc(op.Identity)
}

func (op *FlowAuthorizationOperation) RequireAuthorizedMethod() error {
	return op.RequireMethodFunc(op.Identity, op.Binding.Scope, op.Authorization.Method)
}

type SecurityInputOperation struct {
	Accounts accountstore.Store

	SQL identitystore.Store

	Dependencies
	Identity domain.AuthIdentity
	Input    verify_security_input.VerificationInput
	Binding  domain.VerificationBinding
	Password string
	user     *accountrecord.User
	factor   *identityrecord.TwoFA
	Proof    *dto.SecurityProof
}

func (op *SecurityInputOperation) BindOperation() error {
	var err error
	op.Binding, err = op.BindFunc(domain.VerificationOperation{Scope: op.Input.Scope, Context: op.Input.Context})
	return err
}

func (op *SecurityInputOperation) RequireMethod(method string) error {
	return op.RequireMethodFunc(op.Identity, op.Input.Scope, method)
}

func (op *SecurityInputOperation) PasswordEncryptionEnabled() bool {
	return common.PasswordLoginEncryptionEnabled
}

func (op *SecurityInputOperation) DecryptPassword() error {
	var err error
	op.Password, err = common.DecryptPassword(op.Input.PasswordEncrypted, op.Input.EncryptionKeyID)
	return err
}

func (op *SecurityInputOperation) LoadPassword() error {
	var err error
	op.user, err = op.Accounts.GetUserById(op.Identity.UserID, true)
	return err
}

func (op *SecurityInputOperation) PasswordMatches() bool {
	return op.Password != "" && op.user.Password != "" && common.ValidatePasswordAndHash(op.Password, op.user.Password)
}

func (op *SecurityInputOperation) NumericCodeValid() bool {
	_, err := common.ValidateNumericCode(op.Input.Code)
	return err == nil
}

func (op *SecurityInputOperation) LoadFactor() error {
	var err error
	op.factor, err = op.SQL.GetTwoFAByUserId(op.Identity.UserID)
	return err
}

func (op *SecurityInputOperation) VerifyFactor() error {
	return op.VerifyFactorFunc(op.factor, op.Input.Code)
}

func (op *SecurityInputOperation) Complete() error {
	var err error
	op.Proof, err = op.CompleteFunc(op.Identity, op.Binding, op.Input.Method)
	return err
}

type FactorCodeOperation struct {
	FactorRecordOperation
}

func (op FactorCodeOperation) NumericCode(code string) (string, error) {
	return common.ValidateNumericCode(code)
}

func (op FactorCodeOperation) BackupCodeValid(code string) bool {
	return common.ValidateBackupCode(code)
}

type proofConsumption struct {
	SQL identitystore.Store

	Token    string
	Identity domain.AuthIdentity
	Flow     *identityrecord.AuthFlow
}

func (op *proofConsumption) ConsumeProof() error {
	var err error
	op.Flow, err = op.SQL.ConsumeAuthFlowWithAction(op.Token, identitystore.AuthFlowMatch{
		Purpose: domain.AuthFlowPurposeSecurityProof, UserId: op.Identity.UserID, SessionId: op.Identity.SessionID,
	}, func(tx *gorm.DB, _ *identityrecord.AuthFlow) error {
		return op.SQL.ValidateAuthSessionWithTx(tx, op.Identity)
	})
	return err
}

type FactorRecordOperation struct {
	SQL    identitystore.Store
	Factor *identityrecord.TwoFA
}

func (op FactorRecordOperation) Enabled() bool { return op.Factor != nil && op.Factor.IsEnabled }
func (op FactorRecordOperation) Locked() bool  { return op.Factor.IsLocked() }
func (op FactorRecordOperation) ValidateTOTP(code string) (bool, error) {
	return op.SQL.ValidateTOTPAndUpdateUsage(op.Factor, code)
}
func (op FactorRecordOperation) ValidateBackup(code string) (bool, error) {
	return op.SQL.ValidateBackupCodeAndUpdateUsage(op.Factor, code)
}
func (op FactorRecordOperation) IncrementFailedAttempts() error {
	return op.SQL.IncrementFailedAttempts(op.Factor)
}

func (op *ConsumeProofOperation) SetProofIdentity(identity domain.AuthIdentity) {
	op.proofConsumption.SQL = op.Dependencies.SQL
	op.proofConsumption.Identity = identity
}

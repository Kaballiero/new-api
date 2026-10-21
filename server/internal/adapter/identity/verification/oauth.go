package verification

import (
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/dto"
)

type OAuthVerificationFlow struct {
	Scope           string `json:"scope"`
	ContextHash     string `json:"context_hash"`
	ProviderUserID  string `json:"provider_user_id"`
	UserAuthVersion int64  `json:"auth_version"`
	SessionVersion  int64  `json:"session_version"`
}
type StartOAuth struct {
	Identity  domain.AuthIdentity
	Operation domain.VerificationOperation
	Provider  string
	BindFunc  func(domain.VerificationOperation) (domain.VerificationBinding, error)
	FindFunc  func(domain.AuthIdentity, string, string) (string, error)
	Result    *OAuthVerificationFlow
	binding   domain.VerificationBinding
	account   string
}

func (op *StartOAuth) Bind() error {
	var err error
	op.binding, err = op.BindFunc(op.Operation)
	return err
}
func (op *StartOAuth) FindLinkedAccount() error {
	var err error
	op.account, err = op.FindFunc(op.Identity, op.binding.Scope, op.Provider)
	return err
}
func (op *StartOAuth) Capture() {
	op.Result = &OAuthVerificationFlow{Scope: op.binding.Scope, ContextHash: op.binding.ContextHash, ProviderUserID: op.account, UserAuthVersion: op.Identity.UserAuthVersion, SessionVersion: op.Identity.SessionVersion}
}

type FinishOAuth struct {
	Identity               domain.AuthIdentity
	Provider, ActualUserID string
	Flow                   *OAuthVerificationFlow
	FindFunc               func(domain.AuthIdentity, string, string) (string, error)
	CompleteFunc           func(domain.AuthIdentity, domain.VerificationBinding, string) (*dto.SecurityProof, error)
	Result                 *dto.SecurityProof
	expectedID             string
}

func (op *FinishOAuth) FlowValid() bool {
	return op.Flow != nil && op.Flow.ContextHash != "" && op.Flow.UserAuthVersion == op.Identity.UserAuthVersion && op.Flow.SessionVersion == op.Identity.SessionVersion
}
func (op *FinishOAuth) FindLinkedAccount() error {
	var err error
	op.expectedID, err = op.FindFunc(op.Identity, op.Flow.Scope, op.Provider)
	return err
}
func (op *FinishOAuth) AccountMatches() bool {
	return op.ActualUserID != "" && op.ActualUserID == op.expectedID && op.ActualUserID == op.Flow.ProviderUserID
}
func (op *FinishOAuth) IssueProof() error {
	var err error
	op.Result, err = op.CompleteFunc(op.Identity, domain.VerificationBinding{Scope: op.Flow.Scope, ContextHash: op.Flow.ContextHash}, "oauth")
	return err
}

package start_oauth_flow

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

type fakeOperation struct {
	known, identity, accepted, telegram bool
	proofErr, createErr                 error
	calls                               []string
}

func (f *fakeOperation) ProviderKnown() bool { return f.known }
func (f *fakeOperation) InitializePayload()  { f.calls = append(f.calls, "init") }
func (f *fakeOperation) NewTelegramFlow() error {
	f.calls = append(f.calls, "telegram")
	return nil
}
func (f *fakeOperation) ReadIdentity() bool { return f.identity }
func (f *fakeOperation) BeginBindingAudit() { f.calls = append(f.calls, "audit") }
func (f *fakeOperation) RequireBindingProof() (bool, error) {
	f.calls = append(f.calls, "proof")
	return f.accepted, f.proofErr
}
func (f *fakeOperation) HasTelegram() bool      { return f.telegram }
func (f *fakeOperation) ValidateSession() error { return nil }
func (f *fakeOperation) CaptureSession()        { f.calls = append(f.calls, "capture") }
func (f *fakeOperation) StartVerification() error {
	f.calls = append(f.calls, "verify")
	return nil
}
func (f *fakeOperation) CreateFlow() error {
	f.calls = append(f.calls, "create")
	return f.createErr
}
func (f *fakeOperation) MarkSucceeded() { f.calls = append(f.calls, "succeeded") }

func TestExecute(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name    string
		op      fakeOperation
		request Request
		want    error
		calls   []string
	}{
		{"unknown provider", fakeOperation{}, Request{Provider: "x", Intent: "login"}, domain.ErrInvalidInput, nil},
		{"unknown intent", fakeOperation{known: true}, Request{Provider: "github", Intent: "other"}, domain.ErrInvalidInput, nil},
		{"aff for bind", fakeOperation{known: true}, Request{Provider: "github", Intent: "bind", Aff: "a"}, domain.ErrInvalidInput, nil},
		{"scope for login", fakeOperation{known: true}, Request{Provider: "github", Intent: "login", Scope: "s"}, domain.ErrInvalidInput, nil},
		{"context for bind", fakeOperation{known: true}, Request{Provider: "github", Intent: "bind", Context: json.RawMessage(`{}`)}, domain.ErrInvalidInput, nil},
		{"bind without session", fakeOperation{known: true}, Request{Provider: "github", Intent: "bind"}, domain.ErrBindingSessionMissing, []string{"init"}},
		{"proof rejected", fakeOperation{known: true, identity: true}, Request{Provider: "github", Intent: "bind"}, domain.ErrStopped, []string{"init", "audit", "proof"}},
		{"proof failure", fakeOperation{known: true, identity: true, proofErr: storage}, Request{Provider: "github", Intent: "bind"}, storage, []string{"init", "audit", "proof"}},
		{"create failure", fakeOperation{known: true, createErr: storage}, Request{Provider: " github ", Intent: " login "}, storage, []string{"init", "create"}},
		{"login", fakeOperation{known: true}, Request{Provider: "telegram", Intent: "login", Aff: " code "}, nil, []string{"init", "telegram", "create", "succeeded"}},
		{"bind", fakeOperation{known: true, identity: true, accepted: true}, Request{Provider: "github", Intent: "bind"}, nil, []string{"init", "audit", "proof", "capture", "create", "succeeded"}},
		{"verify", fakeOperation{known: true, identity: true}, Request{Provider: "github", Intent: "verify", Scope: "s"}, nil, []string{"init", "verify", "create", "succeeded"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			request := tc.request
			err := NewUsecase(&op).Execute(&request)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, tc.calls, op.calls)
		})
	}
}

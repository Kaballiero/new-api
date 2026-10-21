package request_password_reset

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
)

var errNotFound = errors.New("email not found")

type fakeOperation struct {
	valid      bool
	lookupErr  error
	deliverErr error
	registered []string
	logs       []string
	lang       string
}

func (f *fakeOperation) NormalizeEmail(raw string) string { return raw }
func (f *fakeOperation) ValidEmail(string) bool           { return f.valid }
func (f *fakeOperation) LookupAccount(string) error       { return f.lookupErr }
func (f *fakeOperation) EmailNotFound(err error) bool     { return errors.Is(err, errNotFound) }
func (f *fakeOperation) GenerateCode(int) string          { return "code" }
func (f *fakeOperation) RegisterCode(email, code string) {
	f.registered = append(f.registered, email+"|"+code)
}
func (f *fakeOperation) Deliver(_, _, lang string) error {
	f.lang = lang
	return f.deliverErr
}
func (f *fakeOperation) LogDeliveryFailure(email string, err error) {
	f.logs = append(f.logs, "delivery "+email+": "+err.Error())
}
func (f *fakeOperation) LogLookupFailure(email string, err error) {
	f.logs = append(f.logs, "lookup "+email+": "+err.Error())
}

// Only an invalid address is reported; unknown accounts and failures look
// like success to the client and are logged instead.
func TestExecute(t *testing.T) {
	cases := []struct {
		name       string
		op         fakeOperation
		want       error
		registered []string
		logs       []string
	}{
		{"invalid", fakeOperation{}, domain.ErrInvalidInput, nil, nil},
		{"unknown account", fakeOperation{valid: true, lookupErr: errNotFound}, nil, nil, nil},
		{"lookup failure", fakeOperation{valid: true, lookupErr: errors.New("ambiguous")}, nil, nil, []string{"lookup a@b.c: ambiguous"}},
		{"delivery failure", fakeOperation{valid: true, deliverErr: errors.New("smtp")}, nil, []string{"a@b.c|code"}, []string{"delivery a@b.c: smtp"}},
		{"sent", fakeOperation{valid: true}, nil, []string{"a@b.c|code"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			err := NewUsecase(&op).Execute(Input{Email: "a@b.c", Lang: "ru"})
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, tc.registered, op.registered)
			assert.Equal(t, tc.logs, op.logs)
			if tc.registered != nil {
				assert.Equal(t, "ru", op.lang, "the e-mail is sent in the request language")
			}
		})
	}
}

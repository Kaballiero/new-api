package register_password

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTaken = errors.New("email already taken")

type fakeOperation struct {
	validateErr  error
	verification bool
	codeValid    bool
	ensureErr    error
	exists       bool
	existsErr    error
	insertErr    error
	findErr      error
	defaultToken bool
	keyErr       error
	tokenErr     error
	inserted     []string
	tokens       []string
}

func (f *fakeOperation) NormalizeEmail(email string) string { return email }
func (f *fakeOperation) Validate(User) error                { return f.validateErr }
func (f *fakeOperation) EmailVerificationEnabled() bool     { return f.verification }
func (f *fakeOperation) VerifyEmail(string, string) bool    { return f.codeValid }
func (f *fakeOperation) EnsureEmailAvailable(string) error  { return f.ensureErr }
func (f *fakeOperation) EmailTaken(err error) bool          { return errors.Is(err, errTaken) }
func (f *fakeOperation) CheckExists(string, string) (bool, error) {
	return f.exists, f.existsErr
}
func (f *fakeOperation) Inviter(string) (int, error) { return 0, nil }
func (f *fakeOperation) Insert(username, _, email string, _ int, lang string) error {
	f.inserted = append(f.inserted, username+"|"+email+"|"+lang)
	return f.insertErr
}
func (f *fakeOperation) FindInsertedID(string) (int, error) { return 11, f.findErr }
func (f *fakeOperation) DefaultTokenEnabled() bool          { return f.defaultToken }
func (f *fakeOperation) GenerateTokenKey() (string, error)  { return "key", f.keyErr }
func (f *fakeOperation) InsertDefaultToken(id int, username, key, lang string) error {
	f.tokens = append(f.tokens, username+"|"+key+"|"+lang)
	return f.tokenErr
}

func TestExecuteRejections(t *testing.T) {
	storage := errors.New("storage")
	cases := []struct {
		name string
		op   fakeOperation
		user User
		want error
		log  string
	}{
		{"blank username", fakeOperation{}, User{Username: "  "}, domain.ErrInvalidInput, ""},
		{"validator", fakeOperation{validateErr: errors.New("too long")}, User{Username: "u"}, &domain.InputError{Detail: "too long"}, ""},
		{"verification fields", fakeOperation{verification: true}, User{Username: "u", Email: "a@b.c"}, domain.ErrEmailVerificationRequired, ""},
		{"verification code", fakeOperation{verification: true}, User{Username: "u", Email: "a@b.c", VerificationCode: "1"}, domain.ErrVerificationCodeInvalid, ""},
		{"verified email taken", fakeOperation{verification: true, codeValid: true, ensureErr: errTaken}, User{Username: "u", Email: "a@b.c", VerificationCode: "1"}, domain.ErrEmailTaken, ""},
		{"verified email storage", fakeOperation{verification: true, codeValid: true, ensureErr: storage}, User{Username: "u", Email: "a@b.c", VerificationCode: "1"}, domain.ErrStorageUnavailable, ""},
		{"exists check", fakeOperation{existsErr: storage}, User{Username: "u"}, domain.ErrStorageUnavailable, "CheckUserExistOrDeleted error: storage"},
		{"exists", fakeOperation{exists: true}, User{Username: "u"}, domain.ErrUserExists, ""},
		{"insert email taken", fakeOperation{insertErr: errTaken}, User{Username: "u"}, domain.ErrEmailTaken, ""},
		{"insert failure", fakeOperation{insertErr: storage}, User{Username: "u"}, storage, ""},
		{"find id", fakeOperation{findErr: storage}, User{Username: "u"}, domain.ErrRegisterFailed, ""},
		{"token key", fakeOperation{defaultToken: true, keyErr: storage}, User{Username: "u"}, domain.ErrDefaultTokenFailed, "failed to generate token key: storage"},
		{"token insert", fakeOperation{defaultToken: true, tokenErr: storage}, User{Username: "u"}, domain.ErrDefaultTokenInsertFailed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			err := NewUsecase(&op).Execute(Input{User: tc.user})
			var input *domain.InputError
			if want, ok := tc.want.(*domain.InputError); ok {
				require.ErrorAs(t, err, &input)
				assert.Equal(t, want.Detail, input.Detail)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			var logged *domain.LoggedError
			if tc.log == "" {
				assert.False(t, errors.As(err, &logged))
			} else {
				require.ErrorAs(t, err, &logged)
				assert.Equal(t, tc.log, logged.Log)
			}
		})
	}
}

func TestExecuteRegisters(t *testing.T) {
	op := fakeOperation{verification: true, codeValid: true, defaultToken: true}
	require.NoError(t, NewUsecase(&op).Execute(Input{User: User{Username: " u ", Email: "a@b.c", VerificationCode: "1"}, Lang: "ru"}))
	assert.Equal(t, []string{"u|a@b.c|ru"}, op.inserted, "the request language reaches the persisted registration texts")
	assert.Equal(t, []string{"u|key|ru"}, op.tokens)

	op = fakeOperation{}
	require.NoError(t, NewUsecase(&op).Execute(Input{User: User{Username: "u", Email: "ignored@b.c"}}))
	assert.Equal(t, []string{"u||"}, op.inserted, "the e-mail is stored only when it is verified")
	assert.Empty(t, op.tokens)
}

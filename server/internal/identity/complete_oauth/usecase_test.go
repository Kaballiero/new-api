package complete_oauth

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBinding struct {
	identity                   bool
	decodeErr, commitErr       error
	loadErr                    error
	providerTaken, legacyTaken bool
	notifyFailed               bool
	committed                  bool
}

func (f *fakeBinding) ReadBindingIdentity() bool           { return f.identity }
func (f *fakeBinding) DecodeBindingPayload() error         { return f.decodeErr }
func (f *fakeBinding) ValidateBindingAuthorization() error { return nil }
func (f *fakeBinding) ProviderUserTaken() bool             { return f.providerTaken }
func (f *fakeBinding) LegacyUserTaken() bool               { return f.legacyTaken }
func (f *fakeBinding) CommitBinding() error {
	f.committed = true
	return f.commitErr
}
func (f *fakeBinding) LoadBoundAccount() error  { return f.loadErr }
func (f *fakeBinding) NotifyBoundAccount() bool { return f.notifyFailed }

func TestCompleteBinding(t *testing.T) {
	storage := errors.New("storage")

	_, err := CompleteBinding(&fakeBinding{})
	assert.ErrorIs(t, err, domain.ErrAuthTokenInvalid)

	op := &fakeBinding{identity: true, legacyTaken: true}
	_, err = CompleteBinding(op)
	assert.ErrorIs(t, err, domain.ErrAlreadyBound)
	assert.False(t, op.committed)

	result, err := CompleteBinding(&fakeBinding{identity: true, commitErr: storage})
	assert.ErrorIs(t, err, storage)
	assert.Equal(t, Result{}, result)

	result, err = CompleteBinding(&fakeBinding{identity: true, loadErr: storage})
	assert.ErrorIs(t, err, storage)
	assert.Equal(t, Result{BindingSucceeded: true, NotificationWarning: true}, result, "the audit reports a committed binding")

	result, err = CompleteBinding(&fakeBinding{identity: true, notifyFailed: true})
	require.NoError(t, err)
	assert.Equal(t, Result{Outcome: OutcomeBound, BindingSucceeded: true, NotificationWarning: true}, result)
}

var (
	errMissing = errors.New("record not found")
	errTaken   = errors.New("email already taken")
)

type fakeAccounts struct {
	profile        ProviderProfile
	column         string
	fillErr        error
	taken          map[string]bool
	accountID      int
	registration   bool
	ensureErr      error
	transactionErr error
	migrated       []string
	created        bool
}

func (f *fakeAccounts) Profile() ProviderProfile    { return f.profile }
func (f *fakeAccounts) ProviderColumn() string      { return f.column }
func (f *fakeAccounts) FillByID(string) error       { return f.fillErr }
func (f *fakeAccounts) Missing(err error) bool      { return errors.Is(err, errMissing) }
func (f *fakeAccounts) Taken(id string) bool        { return f.taken[id] }
func (f *fakeAccounts) AccountID() int              { return f.accountID }
func (f *fakeAccounts) LogMigration(string, string) {}
func (f *fakeAccounts) MigrateGitHub(id string) error {
	f.migrated = append(f.migrated, id)
	return nil
}
func (f *fakeAccounts) LogMigrationFailure(error)           {}
func (f *fakeAccounts) RegistrationEnabled() bool           { return f.registration }
func (f *fakeAccounts) PrepareDefaultUsername()             {}
func (f *fakeAccounts) UsernameExists(string) (bool, error) { return false, nil }
func (f *fakeAccounts) UsernameMaximum() int                { return 20 }
func (f *fakeAccounts) SetUsername(string)                  {}
func (f *fakeAccounts) SetDisplayName(string)               {}
func (f *fakeAccounts) ProviderName() string                { return "GitHub" }
func (f *fakeAccounts) SetNormalizedEmail(string)           {}
func (f *fakeAccounts) EnsureEmail() error                  { return f.ensureErr }
func (f *fakeAccounts) EmailTaken(err error) bool           { return errors.Is(err, errTaken) }
func (f *fakeAccounts) SetRoleAndStatus()                   {}
func (f *fakeAccounts) FindInviter(string) (int, error)     { return 0, nil }
func (f *fakeAccounts) Finalize(int)                        {}
func (f *fakeAccounts) Transaction(run func(AccountCreation) error) error {
	f.created = true
	return f.transactionErr
}

func TestResolveAccount(t *testing.T) {
	cases := []struct {
		name    string
		op      fakeAccounts
		want    error
		created bool
	}{
		{"telegram unbound", fakeAccounts{column: "telegram_id", fillErr: errMissing}, domain.ErrTelegramAccountNotBound, false},
		{"deleted account", fakeAccounts{profile: ProviderProfile{ID: "1"}, taken: map[string]bool{"1": true}}, domain.ErrAccountDeleted, false},
		{"existing account", fakeAccounts{profile: ProviderProfile{ID: "1"}, taken: map[string]bool{"1": true}, accountID: 5}, nil, false},
		{"registration disabled", fakeAccounts{profile: ProviderProfile{ID: "1"}}, domain.ErrRegistrationDisabled, false},
		{"email taken", fakeAccounts{profile: ProviderProfile{ID: "1", Email: "a@b.c"}, registration: true, ensureErr: errTaken}, domain.ErrEmailTaken, false},
		{"registered", fakeAccounts{profile: ProviderProfile{ID: "1"}, registration: true}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := tc.op
			err := ResolveAccount("", &op, &op)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, tc.created, op.created)
		})
	}

	legacy := fakeAccounts{profile: ProviderProfile{ID: "new", LegacyID: "old"}, taken: map[string]bool{"old": true}, accountID: 5}
	require.NoError(t, ResolveAccount("", &legacy, &legacy))
	assert.Equal(t, []string{"new"}, legacy.migrated, "a legacy GitHub id is migrated to the new id")
}

package get_status

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/personal_access_tokens/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errMissing = errors.New("missing")

type fakeReader struct {
	account  domain.Account
	usage    domain.Usage
	usageErr error
	queried  bool
	roles    []int
	ref      string
}

func (r *fakeReader) Account(int) (domain.Account, error) { return r.account, nil }
func (r *fakeReader) LatestUsage(_ int, ref string, roles []int) (domain.Usage, error) {
	r.queried, r.ref, r.roles = true, ref, roles
	return r.usage, r.usageErr
}
func (r *fakeReader) IsMissing(err error) bool { return errors.Is(err, errMissing) }

var roles = domain.AuditRoles{Root: 100, Common: 1, Admin: 10}

func execute(reader *fakeReader) (*Output, error) {
	return NewUsecase(reader, domain.Fingerprint, roles).Execute(Input{UserID: 3})
}

// Mirrors controller TestAccessTokenLifecycleAndLateRequests and
// TestAuditRoleVisibilityAndPermissions at the usecase level.
func TestGetStatus(t *testing.T) {
	reader := &fakeReader{}
	status, err := execute(reader)
	require.NoError(t, err)
	assert.Equal(t, &Output{}, status)
	assert.False(t, reader.queried, "no token means no usage lookup")

	created := int64(50)
	reader = &fakeReader{account: domain.Account{Token: "pat ", Role: 10, CreatedAt: &created}, usage: domain.Usage{CreatedAt: 60, Ip: "192.0.2.8"}}
	status, err = execute(reader)
	require.NoError(t, err)
	used := int64(60)
	assert.Equal(t, &Output{Exists: true, TokenRef: domain.Fingerprint("pat"), CreatedAt: &created, LastUsedAt: &used, LastUsedIp: "192.0.2.8"}, status)
	assert.Equal(t, domain.Fingerprint("pat"), reader.ref)
	assert.Equal(t, []int{1, 10}, reader.roles, "non-root owners only see usage by common and admin actors")

	reader = &fakeReader{account: domain.Account{Token: "pat", Role: 100}, usageErr: errMissing}
	status, err = execute(reader)
	require.NoError(t, err)
	assert.Nil(t, reader.roles, "root owners see usage by any actor")
	assert.Equal(t, &Output{Exists: true, TokenRef: domain.Fingerprint("pat")}, status, "never used")
}

func TestGetStatusUsageFailure(t *testing.T) {
	cause := errors.New("audit store unavailable")
	_, err := execute(&fakeReader{account: domain.Account{Token: "pat"}, usageErr: cause})
	require.ErrorIs(t, err, domain.ErrInternal, "an audit query failure must not look like never used")
	require.ErrorIs(t, err, cause)
}

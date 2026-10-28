package revoke_session

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRevoker struct {
	revoked bool
	err     error
	calls   []string
}

func (f *fakeRevoker) Revoke(_ int, sid, reason string) (bool, error) {
	f.calls = append(f.calls, sid+"/"+reason)
	return f.revoked, f.err
}

func TestExecute(t *testing.T) {
	revoker := &fakeRevoker{}
	_, err := NewUsecase(revoker).Execute(Input{UserID: 1, SID: "  "})
	assert.ErrorIs(t, err, domain.ErrSessionIDRequired)
	assert.Empty(t, revoker.calls)

	_, err = NewUsecase(revoker).Execute(Input{UserID: 1, SID: "sid"})
	assert.ErrorIs(t, err, domain.ErrSessionNotFound)

	storage := errors.New("storage")
	_, err = NewUsecase(&fakeRevoker{revoked: true, err: storage}).Execute(Input{UserID: 1, SID: "sid"})
	assert.ErrorIs(t, err, storage)

	revoker = &fakeRevoker{revoked: true}
	sid, err := NewUsecase(revoker).Execute(Input{UserID: 1, SID: " sid "})
	require.NoError(t, err)
	assert.Equal(t, "sid", sid)
	assert.Equal(t, []string{"sid/user_revoked"}, revoker.calls)
}

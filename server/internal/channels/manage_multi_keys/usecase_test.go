package manage_multi_keys

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	channel *domain.Channel
	getErr  error
	updated *domain.Channel
}

func (s *fakeStore) Get(int, bool) (*domain.Channel, error) { return s.channel, s.getErr }
func (s *fakeStore) Keys(channel *domain.Channel) []string {
	return strings.Split(channel.Key, "\n")
}
func (s *fakeStore) Update(channel *domain.Channel) error { s.updated = channel; return nil }
func (s *fakeStore) LockPolling(int) func()               { return func() {} }

func multiKeyChannel(statuses map[int]int) *domain.Channel {
	return &domain.Channel{Id: 7, Key: "k0\nk1\nk2", ChannelInfo: domain.ChannelInfo{IsMultiKey: true, MultiKeySize: 3, MultiKeyStatusList: statuses}}
}

func run(store *fakeStore, request MultiKeyManageRequest, sensitive bool) (Output, error, bool) {
	refreshed := false
	output, err := NewUsecase(store, func() { refreshed = true }, func() bool { return sensitive }, func() {}, func(string, int) {}).Execute(request)
	return output, err, refreshed
}

func TestManageMultiKeysRejections(t *testing.T) {
	index := 5
	cases := []struct {
		name      string
		store     *fakeStore
		request   MultiKeyManageRequest
		sensitive bool
		want      error
	}{
		{"missing channel", &fakeStore{getErr: errors.New("record not found")}, MultiKeyManageRequest{Action: "get_key_status"}, true, domain.ErrMultiKeyChannelNotFound},
		{"single key channel", &fakeStore{channel: &domain.Channel{Id: 7}}, MultiKeyManageRequest{Action: "get_key_status"}, true, domain.ErrNotMultiKey},
		{"delete without permission", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "delete_key"}, false, domain.ErrSensitiveWriteDenied},
		{"disable without index", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "disable_key"}, true, domain.ErrDisableKeyIndexRequired},
		{"enable without index", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "enable_key"}, true, domain.ErrEnableKeyIndexRequired},
		{"delete without index", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "delete_key"}, true, domain.ErrDeleteKeyIndexRequired},
		{"index out of range", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "disable_key", KeyIndex: &index}, true, domain.ErrKeyIndexOutOfRange},
		{"nothing to disable", &fakeStore{channel: multiKeyChannel(map[int]int{0: 2, 1: 3, 2: 2})}, MultiKeyManageRequest{Action: "disable_all_keys"}, true, domain.ErrNoKeysToDisable},
		{"nothing auto disabled", &fakeStore{channel: multiKeyChannel(map[int]int{0: 2})}, MultiKeyManageRequest{Action: "delete_disabled_keys"}, true, domain.ErrNoAutoDisabledKeys},
		{"unknown action", &fakeStore{channel: multiKeyChannel(nil)}, MultiKeyManageRequest{Action: "rotate"}, true, domain.ErrMultiKeyActionUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err, refreshed := run(tc.store, tc.request, tc.sensitive)
			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, tc.store.updated)
			assert.False(t, refreshed)
		})
	}
}

func TestManageMultiKeysOutcomes(t *testing.T) {
	store := &fakeStore{channel: multiKeyChannel(map[int]int{0: 2, 1: 3})}
	output, err, refreshed := run(store, MultiKeyManageRequest{Action: "disable_all_keys"}, true)
	require.NoError(t, err)
	assert.Equal(t, Output{Outcome: KeysDisabled, Count: 1}, output)
	assert.True(t, refreshed)
	assert.Equal(t, map[int]int{0: 2, 1: 3, 2: 2}, store.updated.ChannelInfo.MultiKeyStatusList)

	store = &fakeStore{channel: multiKeyChannel(map[int]int{1: 3})}
	store.channel.ChannelInfo.MultiKeyDisabledReason = map[int]string{1: "quota"}
	output, err, _ = run(store, MultiKeyManageRequest{Action: "delete_disabled_keys"}, true)
	require.NoError(t, err)
	assert.Equal(t, Output{Outcome: AutoDisabledKeysDeleted, Count: 1, Data: 1}, output)
	assert.Equal(t, "k0\nk2", store.updated.Key)
	assert.Equal(t, 2, store.updated.ChannelInfo.MultiKeySize)
	assert.Empty(t, store.updated.ChannelInfo.MultiKeyDisabledReason)

	store = &fakeStore{channel: multiKeyChannel(map[int]int{0: 2})}
	status := 2
	output, err, refreshed = run(store, MultiKeyManageRequest{Action: "get_key_status", Status: &status}, false)
	require.NoError(t, err)
	assert.False(t, refreshed)
	assert.Equal(t, StatusListed, output.Outcome)
	listing := output.Data.(MultiKeyStatusResponse)
	assert.Equal(t, 1, listing.Total)
	assert.Equal(t, 2, listing.EnabledCount)
	assert.Equal(t, 1, listing.ManualDisabledCount)
}

package validate_key_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/apikeys/validate_key"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyStore struct {
	token      *domain.Token
	lookupErr  error
	ownerErr   error
	updateErr  error
	saved      []int
	ownerReads int
	admin      bool
}

func (s *keyStore) Lookup(string) (*domain.Token, error) { return s.token, s.lookupErr }
func (s *keyStore) UpdateStatus(token *domain.Token) error {
	s.saved = append(s.saved, token.Status)
	return s.updateErr
}
func (*keyStore) IsMissing(err error) bool { return errors.Is(err, errMissing) }
func (s *keyStore) Owner(int) (*domain.Owner, error) {
	s.ownerReads++
	return &domain.Owner{ID: 1, Status: 1, Group: "default"}, s.ownerErr
}
func (s *keyStore) IsAdmin(int) bool                        { return s.admin }
func (*keyStore) AutoGroups(*domain.Token) ([]string, bool) { return nil, false }

func keyRules(cache bool) validate_key.Rules {
	return validate_key.Rules{
		Now: func() int64 { return 100 }, CacheEnabled: func() bool { return cache },
		UsableGroups: func(string) map[string]string { return map[string]string{"default": "", "auto": "", "old": ""} },
		GroupExists:  func(group string) bool { return group == "default" },
		IPAllowed:    func(net.IP, []string) bool { return false },
		Log:          func(string) {}, Debug: func(context.Context, string, ...any) {},
	}
}

// TestKeyStatusWriteBack preserves the legacy boundary at now and the difference
// between cached validation and durable status changes, including failed writes.
func TestKeyStatusWriteBack(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		status                                 int
		expiry                                 int64
		quota                                  int
		unlimited, cache, readOnly, writeFails bool
		wantErr                                error
		wantStatus                             int
		wantWrite                              bool
	}{
		{name: "valid", status: 1, expiry: -1, quota: 1, wantStatus: 1},
		{name: "expires_at_now", status: 1, expiry: 100, quota: 1, wantStatus: 1},
		{name: "expired", status: 1, expiry: 99, quota: 1, wantErr: domain.ErrKeyInvalid, wantStatus: 3, wantWrite: true},
		{name: "exhausted", status: 1, expiry: -1, wantErr: domain.ErrKeyInvalid, wantStatus: 4, wantWrite: true},
		{name: "unlimited_zero", status: 1, expiry: -1, unlimited: true, wantStatus: 1},
		{name: "expiry_before_quota", status: 1, expiry: 99, wantErr: domain.ErrKeyInvalid, wantStatus: 3, wantWrite: true},
		{name: "cached_expired", status: 1, expiry: 99, quota: 1, cache: true, wantErr: domain.ErrKeyInvalid, wantStatus: 1},
		{name: "cached_exhausted", status: 1, expiry: -1, cache: true, wantErr: domain.ErrKeyInvalid, wantStatus: 1},
		{name: "write_failure_still_rejects", status: 1, expiry: 99, quota: 1, writeFails: true, wantErr: domain.ErrKeyInvalid, wantStatus: 3, wantWrite: true},
		{name: "disabled", status: 2, expiry: -1, quota: 1, wantErr: domain.ErrKeyInvalid, wantStatus: 2},
		{name: "already_expired", status: 3, expiry: -1, quota: 1, wantErr: domain.ErrKeyInvalid, wantStatus: 3},
		{name: "read_expired", status: 3, expiry: 99, readOnly: true, wantStatus: 3},
		{name: "read_exhausted", status: 4, expiry: -1, readOnly: true, wantStatus: 4},
		{name: "read_disabled", status: 2, expiry: -1, quota: 1, readOnly: true, wantErr: domain.ErrKeyDisabled, wantStatus: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &keyStore{token: &domain.Token{Id: 9, UserId: 1, Key: "key", Status: tc.status, ExpiredTime: tc.expiry, RemainQuota: tc.quota, UnlimitedQuota: tc.unlimited}}
			if tc.writeFails {
				store.updateErr = errors.New("storage unavailable")
			}
			caller, err := validate_key.NewUsecase(store, keyRules(tc.cache)).Execute(context.Background(), validate_key.Input{Key: "key", ReadOnly: tc.readOnly})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, caller.Token)
			assert.Equal(t, tc.wantStatus, caller.Token.Status)
			if tc.wantWrite {
				assert.Equal(t, []int{tc.wantStatus}, store.saved)
			} else {
				assert.Empty(t, store.saved)
			}
			if tc.wantErr != nil {
				assert.Zero(t, store.ownerReads)
			} else {
				assert.Equal(t, 1, store.ownerReads)
			}
		})
	}
}

func TestKeyRestrictionsAndCompletedContext(t *testing.T) {
	channel42, invalidChannel := "42", "bad"
	for _, tc := range []struct {
		name, ip, group                        string
		pin                                    *string
		admin                                  bool
		wantErr                                error
		ownerReads                             int
		ownerContext, groupContext, keyContext bool
	}{
		{name: "invalid_ip_before_owner", ip: "invalid", wantErr: domain.ErrIPInvalid},
		{name: "denied_ip_before_owner", ip: "192.0.2.1", wantErr: domain.ErrIPDenied},
		{name: "forbidden_group", group: "forbidden", wantErr: domain.ErrGroupForbidden, ownerReads: 1, ownerContext: true},
		{name: "deprecated_group", group: "old", wantErr: domain.ErrGroupDeprecated, ownerReads: 1, ownerContext: true},
		{name: "auto_without_ratio", group: "auto", ownerReads: 1, ownerContext: true, groupContext: true, keyContext: true},
		{name: "non_admin_pin", pin: &channel42, wantErr: domain.ErrChannelForbidden, ownerReads: 1, ownerContext: true, groupContext: true, keyContext: true},
		{name: "invalid_admin_pin", pin: &invalidChannel, admin: true, wantErr: domain.ErrChannelInvalid, ownerReads: 1, ownerContext: true, groupContext: true, keyContext: true},
		{name: "admin_pin", pin: &channel42, admin: true, ownerReads: 1, ownerContext: true, groupContext: true, keyContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := &domain.Token{Id: 1, UserId: 1, Status: 1, ExpiredTime: -1, RemainQuota: 1, Group: tc.group}
			if tc.ip != "" {
				ips := "10.0.0.1"
				token.AllowIps = &ips
			}
			store := &keyStore{token: token, admin: tc.admin}
			caller, err := validate_key.NewUsecase(store, keyRules(false)).Execute(context.Background(), validate_key.Input{Key: "key", ClientIP: tc.ip, Channel: tc.pin})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.ownerReads, store.ownerReads)
			assert.Equal(t, tc.ownerContext, caller.Owner != nil)
			assert.Equal(t, tc.groupContext, caller.UsingGroup != nil)
			assert.Equal(t, tc.keyContext, caller.Context != nil)
			if tc.name == "admin_pin" {
				require.NotNil(t, caller.Context.ChannelID)
				assert.Equal(t, 42, *caller.Context.ChannelID)
			}
		})
	}
}

var errMissing = errors.New("record not found")

func TestKeyLookupFailures(t *testing.T) {
	for _, tc := range []struct {
		name, key                    string
		lookupErr, ownerErr, wantErr error
	}{
		{name: "not_provided", wantErr: domain.ErrKeyNotProvided},
		{name: "missing", key: "key", lookupErr: errMissing, wantErr: domain.ErrKeyInvalid},
		{name: "lookup_failure", key: "key", lookupErr: errors.New("connection reset"), wantErr: domain.ErrInternal},
		{name: "owner_failure", key: "key", ownerErr: errors.New("owner unavailable"), wantErr: domain.ErrInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &keyStore{lookupErr: tc.lookupErr, ownerErr: tc.ownerErr}
			if tc.lookupErr == nil {
				store.token = &domain.Token{Status: 1, ExpiredTime: -1, RemainQuota: 1}
			}
			_, err := validate_key.NewUsecase(store, keyRules(false)).Execute(context.Background(), validate_key.Input{Key: tc.key})
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == domain.ErrInternal {
				cause := tc.lookupErr
				if cause == nil {
					cause = tc.ownerErr
				}
				require.ErrorIs(t, err, cause)
			}
		})
	}
}

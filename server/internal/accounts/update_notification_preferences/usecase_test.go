package update_notification_preferences

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	account          NotificationPreferencesAccount
	loadErr, saveErr error
	saved            *dto.UserSetting
}

func (s *fakeStore) NotificationPreferencesAccount(int) (NotificationPreferencesAccount, error) {
	return s.account, s.loadErr
}
func (s *fakeStore) SaveNotificationPreferences(_ int, settings dto.UserSetting) error {
	s.saved = &settings
	return s.saveErr
}

type user int

func (u user) UserID() int { return int(u) }

func TestValidateNotificationSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings dto.UserSetting
		err      error
	}{
		{name: "unknown type", settings: dto.UserSetting{NotifyType: "sms", QuotaWarningThreshold: 1}, err: domain.ErrNotifyTypeInvalid},
		{name: "threshold", settings: dto.UserSetting{NotifyType: dto.NotifyTypeEmail}, err: domain.ErrQuotaThresholdNotPositive},
		{name: "webhook empty", settings: dto.UserSetting{NotifyType: dto.NotifyTypeWebhook, QuotaWarningThreshold: 1}, err: domain.ErrWebhookURLEmpty},
		{name: "webhook invalid", settings: dto.UserSetting{NotifyType: dto.NotifyTypeWebhook, QuotaWarningThreshold: 1, WebhookUrl: "not a url"}, err: domain.ErrWebhookURLInvalid},
		{name: "email invalid", settings: dto.UserSetting{NotifyType: dto.NotifyTypeEmail, QuotaWarningThreshold: 1, NotificationEmail: "nobody"}, err: domain.ErrNotificationEmailInvalid},
		{name: "bark empty", settings: dto.UserSetting{NotifyType: dto.NotifyTypeBark, QuotaWarningThreshold: 1}, err: domain.ErrBarkURLEmpty},
		{name: "bark invalid", settings: dto.UserSetting{NotifyType: dto.NotifyTypeBark, QuotaWarningThreshold: 1, BarkUrl: "bad url"}, err: domain.ErrBarkURLInvalid},
		{name: "bark scheme", settings: dto.UserSetting{NotifyType: dto.NotifyTypeBark, QuotaWarningThreshold: 1, BarkUrl: "ftp://bark"}, err: domain.ErrNotificationURLNotHTTP},
		{name: "gotify url empty", settings: dto.UserSetting{NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 1}, err: domain.ErrGotifyURLEmpty},
		{name: "gotify token empty", settings: dto.UserSetting{NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 1, GotifyUrl: "https://gotify"}, err: domain.ErrGotifyTokenEmpty},
		{name: "gotify invalid", settings: dto.UserSetting{NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 1, GotifyUrl: "bad url", GotifyToken: "t"}, err: domain.ErrGotifyURLInvalid},
		{name: "gotify scheme", settings: dto.UserSetting{NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 1, GotifyUrl: "ftp://gotify", GotifyToken: "t"}, err: domain.ErrNotificationURLNotHTTP},
		{name: "valid email", settings: dto.UserSetting{NotifyType: dto.NotifyTypeEmail, QuotaWarningThreshold: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNotificationSettings(tc.settings)
			if tc.err == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestUpdateNotificationPreferences(t *testing.T) {
	storage := errors.New("timeout")
	valid := dto.UserSetting{NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 2, GotifyUrl: "https://gotify", GotifyToken: "t", GotifyPriority: 11}
	enabled := true

	t.Run("invalid settings are not loaded", func(t *testing.T) {
		store := &fakeStore{loadErr: storage}
		require.ErrorIs(t, NewUsecase(store).Execute(dto.UserSetting{NotifyType: "sms"}, nil, user(7)), domain.ErrNotifyTypeInvalid)
		assert.Nil(t, store.saved)
	})
	t.Run("load failure stays unclassified", func(t *testing.T) {
		err := NewUsecase(&fakeStore{loadErr: storage}).Execute(valid, nil, user(7))
		require.ErrorIs(t, err, storage)
		assert.NotErrorIs(t, err, domain.ErrSettingsSaveFailed)
	})
	t.Run("save failure", func(t *testing.T) {
		require.ErrorIs(t, NewUsecase(&fakeStore{saveErr: storage}).Execute(valid, nil, user(7)), domain.ErrSettingsSaveFailed)
	})
	t.Run("upstream flag needs an administrator", func(t *testing.T) {
		for role, want := range map[int]bool{1: false, 10: true} {
			store := &fakeStore{account: NotificationPreferencesAccount{ID: 7, Role: role}}
			require.NoError(t, NewUsecase(store).Execute(valid, &enabled, user(7)))
			assert.Equal(t, want, store.saved.UpstreamModelUpdateNotifyEnabled)
			assert.Equal(t, 5, store.saved.GotifyPriority, "out-of-range priority falls back to 5")
		}
	})
}

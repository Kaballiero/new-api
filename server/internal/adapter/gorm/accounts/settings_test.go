package accountsadapter

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	profiledto "github.com/QuantumNous/new-api/internal/accounts/dto"
	accountscodec "github.com/QuantumNous/new-api/internal/adapter/accounts"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountSettingsWireParity(t *testing.T) {
	for _, wire := range []dto.UserSetting{
		{},
		{
			NotifyType: dto.NotifyTypeGotify, QuotaWarningThreshold: 0.25,
			WebhookUrl: "https://webhook.example", WebhookSecret: "fixture-secret",
			NotificationEmail: "fixture@example.com", BarkUrl: "https://bark.example",
			GotifyUrl: "https://gotify.example", GotifyToken: "fixture-token", GotifyPriority: 7,
			UpstreamModelUpdateNotifyEnabled: true, AcceptUnsetRatioModel: true, RecordIpLog: true,
			SidebarModules: "{}", BillingPreference: "wallet_only", Language: "ru",
		},
	} {
		original, err := common.Marshal(wire)
		require.NoError(t, err)
		settings := settingsFromWire(wire)
		// A profile preference write must keep every other stored field and the wire encoder's bytes.
		settings.Language = "ja"
		restored := settingsToWire(settings)
		wire.Language = "ja"
		want, err := common.Marshal(wire)
		require.NoError(t, err)
		actual, err := common.Marshal(restored)
		require.NoError(t, err)
		assert.Equal(t, string(want), string(actual))
		// Zero priority has no omitempty; the zero-value roundtrip must still emit it.
		if wire.NotifyType == "" {
			assert.Equal(t, `{"gotify_priority":0}`, string(original))
		}
	}
}

func TestProfileSettingsCodecParity(t *testing.T) {
	for _, raw := range []string{
		"", "null", "{}", `{"sidebar_modules":"{}","unknown":false}`,
		`{"sidebar_modules":"{}","gotify_priority":"bad"}`, `{"sidebar_modules":`,
	} {
		var legacy dto.UserSetting
		if raw != "" {
			_ = common.Unmarshal([]byte(raw), &legacy)
		}
		decoded := false
		profile := domain.SelfProfile{Id: 7, Role: 1, Setting: raw}
		data := profiledto.BuildSelfUserData(profile, func(value string) string {
			assert.Equal(t, raw, value)
			decoded = true
			return accountscodec.SidebarModules(value)
		}, func(id, role int) any {
			assert.True(t, decoded, "settings decoding precedes capabilities")
			assert.Equal(t, 7, id)
			assert.Equal(t, 1, role)
			return map[string]bool{}
		}, domain.AdminRoles{Admin: 10, Root: 100})
		assert.Equal(t, raw, data["setting"])
		assert.Equal(t, legacy.SidebarModules, data["sidebar_modules"])
		assert.NotContains(t, data, "password")
		assert.NotContains(t, data, "access_token")
	}
}

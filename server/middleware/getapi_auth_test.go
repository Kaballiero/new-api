package middleware

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGetAPIUsageCapabilityConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, config, expected string }{
		{"usage accepted", `[{"integration_id":"usage","principal_user_id":12,"capabilities":["getapi.usage.read"]}]`, "usage"},
		{"missing capability", `[{"integration_id":"usage","principal_user_id":12,"capabilities":["getapi.users.provision"]}]`, ""},
		{"unknown rejects whole config", `[{"integration_id":"usage","principal_user_id":12,"capabilities":["getapi.usage.read"]},{"integration_id":"bad","principal_user_id":13,"capabilities":["getapi.unknown"]}]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GETAPI_INTEGRATIONS", tc.config)
			assert.Equal(t, tc.expected, getAPIIntegrationForPrincipal(12, "getapi.usage.read"))
			assert.Empty(t, getAPIIntegrationForPrincipal(99, "getapi.usage.read"))
		})
	}
}

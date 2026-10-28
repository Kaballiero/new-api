package contracttest

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// NormalizeStripeClientUserAgent masks only host-dependent SDK metadata and
// preserves the SDK's field order and every other JSON value.
func NormalizeStripeClientUserAgent(t testing.TB, value string) string {
	t.Helper()
	decoder := common.NewJSONStreamDecoder(strings.NewReader(value))
	token, err := decoder.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), token)
	var result strings.Builder
	result.WriteByte('{')
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		require.NoError(t, err)
		key, ok := token.(string)
		require.True(t, ok)
		require.False(t, seen[key], "duplicate Stripe metadata field: %s", key)
		if len(seen) > 0 {
			result.WriteByte(',')
		}
		seen[key] = true
		name, err := common.Marshal(key)
		require.NoError(t, err)
		result.Write(name)
		result.WriteByte(':')
		var raw json.RawMessage
		require.NoError(t, decoder.Decode(&raw))
		if key == "uname" || key == "lang_version" {
			var original string
			require.NoError(t, common.Unmarshal(raw, &original))
			replacement := "<host-uname>"
			if key == "lang_version" {
				replacement = "<go-version>"
			}
			raw, err = common.Marshal(replacement)
			require.NoError(t, err)
		}
		result.Write(raw)
	}
	token, err = decoder.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('}'), token)
	require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
	require.True(t, seen["uname"] && seen["lang_version"], "missing Stripe host metadata")
	result.WriteByte('}')
	return result.String()
}

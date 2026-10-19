package provider

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderWireContract(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		conflict         bool
	}{
		{name: "SDK top-level", body: `{"model":"m","provider":{"only":["azure"],"allow_fallbacks":false}}`, want: `{"model":"m","provider":{"only":["azure"],"allow_fallbacks":false}}`},
		{name: "legacy nested", body: `{"model":"m","extra_body":{"provider":{"only":["azure"]},"other":0}}`, want: `{"model":"m","provider":{"only":["azure"]},"extra_body":{"other":0}}`},
		{name: "equal values with reordered keys", body: `{"provider":{"only":[],"allow_fallbacks":false},"extra_body":{"provider":{"allow_fallbacks":false,"only":[]}}}`, want: `{"provider":{"only":[],"allow_fallbacks":false}}`},
		{name: "conflicting executors", body: `{"provider":{"only":["azure"]},"extra_body":{"provider":{"only":["openai"]}}}`, conflict: true},
		{name: "conflicting fallback", body: `{"provider":{"allow_fallbacks":false},"extra_body":{"provider":{"allow_fallbacks":true}}}`, conflict: true},
		{name: "absent differs from empty", body: `{"provider":{},"extra_body":{"provider":{"only":[]}}}`, conflict: true},
		{name: "without provider unchanged", body: `{ "model":"m", "extra_body":[1,2] }`, want: `{ "model":"m", "extra_body":[1,2] }`},
		{name: "explicit null", body: `{"provider":null}`, want: `{"provider":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Normalize([]byte(tc.body))
			if tc.conflict {
				require.ErrorContains(t, err, "conflicting provider")
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(result))
			raw, err := Extract([]byte(tc.body))
			require.NoError(t, err)
			if len(raw) == 0 {
				assert.Equal(t, tc.body, string(result))
			}
		})
	}
}

func TestValidateProviderOperationContract(t *testing.T) {
	for _, tc := range []struct{ name, operation, body, wantError string }{
		{name: "chat preserves preference and fallback", operation: "chat/completions", body: `{"order":["azure","openai"],"ignore":[],"only":[],"allow_fallbacks":true}`},
		{name: "strict embeddings", operation: "embeddings", body: `{"only":["openai"],"allow_fallbacks":false}`},
		{name: "rerank", operation: "rerank", body: `{"only":["cohere"]}`},
		{name: "batch", operation: "batch", body: `{"only":["openai"],"allow_fallbacks":false}`},
		{name: "batch order rejected", operation: "batch", body: `{"order":["openai"]}`, wantError: `unsupported provider field "order"`},
		{name: "TTS policies and provider options", operation: "audio/speech", body: `{"data_collection":"deny","zdr":true,"options":{"elevenlabs":{"voice_settings":{"speed":1}}}}`},
		{name: "TTS selection rejected", operation: "audio/speech", body: `{"only":["elevenlabs"]}`, wantError: `unsupported provider field "only"`},
		{name: "STT selection rejected", operation: "audio/transcriptions", body: `{"allow_fallbacks":false}`, wantError: `unsupported provider field "allow_fallbacks"`},
		{name: "video selection rejected", operation: "videos", body: `{"only":["openai"]}`, wantError: "unsupported"},
		{name: "invalid object", operation: "chat/completions", body: `"azure"`, wantError: "object or null"},
		{name: "invalid list", operation: "chat/completions", body: `{"only":"azure"}`, wantError: "array of strings"},
		{name: "blank provider", operation: "chat/completions", body: `{"only":[" "]}`, wantError: "empty identifier"},
		{name: "invalid fallback", operation: "chat/completions", body: `{"allow_fallbacks":"false"}`, wantError: "boolean"},
		{name: "null preferences", operation: "chat/completions", body: `{"only":null,"allow_fallbacks":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.operation, []byte(tc.body))
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProviderNamespacesStaySeparateForFutureAggregator(t *testing.T) {
	previous := capabilities
	t.Cleanup(func() { capabilities = previous })
	// Test-only aggregator deliberately shares the executor slug "openai", but
	// its routing contract belongs to another namespace and only embeddings.
	const futureChannelType = 10001
	capabilities = append(append([]capability(nil), previous...), capability{channelType: futureChannelType, namespace: "fixture-aggregator", operations: []string{"embeddings"}})
	for _, tc := range []struct {
		namespace, operation string
		want                 []int
	}{
		{NamespaceOpenRouter, "embeddings", []int{constant.ChannelTypeOpenRouter}},
		{"fixture-aggregator", "embeddings", []int{futureChannelType}},
		{"fixture-aggregator", "chat/completions", nil},
		{NamespaceOpenRouter, "videos", nil},
		{"unknown", "embeddings", nil},
	} {
		assert.Equal(t, tc.want, CompatibleChannelTypes(tc.namespace, tc.operation))
	}
}

func TestProviderRejectsDuplicateJSONKeys(t *testing.T) {
	for _, body := range []string{
		`{"provider":{"only":["azure"]},"provider":{"only":["openai"]}}`,
		`{"provider":{"only":["azure"],"only":["openai"]}}`,
		`{"extra_body":{"provider":{"only":["azure"]},"provider":{"only":["openai"]}}}`,
		`{"extra_body":{"provider":{"only":["azure"],"only":["openai"]}}}`,
	} {
		_, err := Extract([]byte(body))
		require.ErrorContains(t, err, "duplicate JSON keys")
	}
	require.Error(t, Validate("chat/completions", []byte(`{"only":["azure"],"only":["openai"]}`)))
}

func TestProviderMultipartFields(t *testing.T) {
	for _, tc := range []struct {
		name            string
		fields          map[string][]string
		want, errorText string
	}{
		{name: "top-level", fields: map[string][]string{"provider": {`{"only":["azure"],"allow_fallbacks":false}`}}, want: `{"only":["azure"],"allow_fallbacks":false}`},
		{name: "legacy nested", fields: map[string][]string{"extra_body": {`{"provider":{"only":["azure"]}}`}}, want: `{"only":["azure"]}`},
		{name: "matching values", fields: map[string][]string{"provider": {`{"only":[]}`}, "extra_body": {`{"provider":{"only":[]}}`}}, want: `{"only":[]}`},
		{name: "conflict", fields: map[string][]string{"provider": {`{"only":["azure"]}`}, "extra_body": {`{"provider":{"only":["openai"]}}`}}, errorText: "conflicting provider"},
		{name: "duplicate provider", fields: map[string][]string{"provider": {`{}`, `{}`}}, errorText: "must be provided once"},
		{name: "no provider", fields: map[string][]string{"model": {"m"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := ExtractForm(tc.fields)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				return
			}
			require.NoError(t, err)
			if tc.want == "" {
				assert.Empty(t, raw)
			} else {
				assert.JSONEq(t, tc.want, string(raw))
			}
		})
	}
}

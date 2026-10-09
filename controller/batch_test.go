package controller

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBatchGoogleOutputBoundary(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"generationConfig":{"maxOutputTokens":8,"candidateCount":1}}`, true},
		{`{"generation_config":{"max_output_tokens":8,"candidate_count":999}}`, false},
		{`{"max_tokens":8}`, false},
		{`{"generationConfig":{"maxOutputTokens":8,"candidate_count":999}}`, false},
		{`{"generationConfig":{"maxOutputTokens":18446744073709551615}}`, false},
	} {
		output, err := validateBatchItem([]byte(tc.body), "gemini-test", "/v1beta/generateContent")
		if tc.valid {
			require.NoError(t, err)
			assert.Equal(t, 8, output)
		} else {
			require.Error(t, err)
		}
	}
}

func TestBatchOutputLimitUsesNativeEndpoint(t *testing.T) {
	for _, tc := range []struct {
		endpoint, body string
		valid          bool
	}{
		{"/v1/chat/completions", `{"max_tokens":8}`, true},
		{"/v1/chat/completions", `{"max_output_tokens":8}`, false},
		{"/v1/responses", `{"max_output_tokens":8}`, true},
		{"/v1/responses", `{"max_tokens":8}`, false},
		{"/v1/messages", `{"max_tokens":8}`, true},
		{"/v1/messages", `{"max_completion_tokens":8}`, false},
	} {
		_, err := validateBatchItem([]byte(tc.body), "test", tc.endpoint)
		if tc.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

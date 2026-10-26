package catalog

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestChannelOwnerNameUsesAdaptorChannelName(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		expected    string
	}{
		{
			name:        "openai",
			channelType: constant.ChannelTypeOpenAI,
			expected:    "openai",
		},
		{
			name:        "codex",
			channelType: constant.ChannelTypeCodex,
			expected:    "codex",
		},
		{
			name:        "openrouter",
			channelType: constant.ChannelTypeOpenRouter,
			expected:    "openrouter",
		},
		{
			name:        "azure fallback",
			channelType: constant.ChannelTypeAzure,
			expected:    "azure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, (Models{}).Owner(tt.channelType))
		})
	}
}

func TestModelProfileFallsBackToCustomForUnknownModels(t *testing.T) {
	model := (Models{}).Profile("custom-test-model")
	require.Equal(t, "custom-test-model", model.Id)
	require.Equal(t, "custom", model.OwnedBy)
}

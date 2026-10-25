package batchaccount

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// BatchChannelConfigHash pins the routing/account configuration without copying
// usable credentials into task snapshots. A changed channel requires explicit
// reconciliation instead of polling a different provider workspace.
func BatchChannelConfigHash(channel *model.Channel) (string, error) {
	data, err := common.Marshal(struct {
		Type         int
		Base         string
		Organization *string
		Headers      *string
		Settings     dto.ChannelSettings
	}{channel.Type, channel.GetBaseURL(), channel.OpenAIOrganization, channel.HeaderOverride, channel.GetSetting()})
	return fmt.Sprintf("%x", sha256.Sum256(data)), err
}

func ValidateBatchChannelHeaders(channel *model.Channel) error {
	for name, value := range channel.GetHeaderOverride() {
		lower := strings.ToLower(strings.TrimSpace(name))
		if name == "*" || strings.HasPrefix(lower, "re:") || strings.HasPrefix(lower, "regex:") {
			return errors.New("Batch requires explicit channel headers rather than request header passthrough")
		}
		text, ok := value.(string)
		if !ok || strings.Contains(text, "{client_header:") {
			return errors.New("Batch channel headers must be static or use {api_key}")
		}
	}
	return nil
}

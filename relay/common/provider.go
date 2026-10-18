package common

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/provider"
	"github.com/gin-gonic/gin"
)

// ApplyRequestOverrides restores provider preferences lost by protocol DTOs
// before channel overrides. The same final JSON is then priced and submitted.
// Other channels retain their existing payload and override behavior.
func ApplyRequestOverrides(c *gin.Context, body []byte, info *RelayInfo) ([]byte, error) {
	var original []byte
	if info.ChannelType == constant.ChannelTypeOpenRouter && strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return nil, err
		}
		original, err = storage.Bytes()
		if err != nil {
			return nil, err
		}
	}
	return ApplyProviderOverrides(body, original, info)
}

// ApplyProviderOverrides uses an explicit JSON source for provider aliases. This
// also lets multipart adapters project their JSON-valued preferences through the
// same administrative override boundary as ordinary JSON requests.
func ApplyProviderOverrides(body, original []byte, info *RelayInfo) ([]byte, error) {
	legacyRouting := false
	if info.ChannelType == constant.ChannelTypeOpenRouter && len(original) > 0 {
		routing, err := provider.Extract(original)
		if err != nil {
			return nil, err
		}
		if len(routing) > 0 {
			var fields map[string]json.RawMessage
			if err = common.Unmarshal(body, &fields); err != nil {
				return nil, err
			}
			fields["provider"] = routing
			var source map[string]json.RawMessage
			if err = common.Unmarshal(original, &source); err != nil {
				return nil, err
			}
			var extra map[string]json.RawMessage
			if common.Unmarshal(source["extra_body"], &extra) == nil {
				legacyRouting = len(extra["provider"]) > 0
				if legacyRouting {
					fields["extra_body"] = source["extra_body"]
				}
			}
			body, err = common.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
	}
	body, err := ApplyParamOverrideWithRelayInfo(body, info)
	if err != nil || info.ChannelType != constant.ChannelTypeOpenRouter {
		return body, err
	}
	if legacyRouting {
		// Keep the old path available to move/copy/delete overrides. Afterwards
		// the canonical top-level value, including an administrator's replacement,
		// is authoritative; the old alias must not conflict with that replacement.
		var fields map[string]json.RawMessage
		if err = common.Unmarshal(body, &fields); err != nil {
			return nil, err
		}
		var extra map[string]json.RawMessage
		if common.Unmarshal(fields["extra_body"], &extra) == nil {
			delete(extra, "provider")
			if len(extra) == 0 {
				delete(fields, "extra_body")
			} else if fields["extra_body"], err = common.Marshal(extra); err != nil {
				return nil, err
			}
			body, err = common.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
	}
	return provider.Normalize(body)
}

package upstream

import (
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/provider"
)

func ApplyOverride(body []byte, overrides map[string]any, ctx domain.OverrideContext) ([]byte, error) {
	info := &relaycommon.RelayInfo{UserId: ctx.UserID, UserGroup: ctx.UserGroup, TokenGroup: ctx.UserGroup, UsingGroup: ctx.UsingGroup, OriginModelName: ctx.Model, RequestURLPath: ctx.Path, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: ctx.Upstream, ParamOverride: overrides}}
	body, err := relaycommon.ApplyParamOverride(body, overrides, relaycommon.BuildParamOverrideContext(info))
	if err != nil {
		return nil, err
	}
	return provider.Normalize(body)
}

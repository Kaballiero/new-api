package service

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	textfilter "github.com/QuantumNous/new-api/pkg/textfilter"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func channelFailurePolicy() domain.FailurePolicy {
	return domain.FailurePolicy{DisableEnabled: func() bool { return common.AutomaticDisableChannelEnabled }, EnableEnabled: func() bool { return common.AutomaticEnableChannelEnabled }, DisableStatus: operation_setting.ShouldDisableByStatusCode, Keywords: func() []string { return operation_setting.AutomaticDisableKeywords }, Search: textfilter.AcSearch}
}

// channelFailure describes a gateway error for the channel failure policy.
func channelFailure(err *types.NewAPIError) *domain.ChannelFailure {
	if err == nil {
		return nil
	}
	return &domain.ChannelFailure{Message: err.Error(), Status: err.StatusCode, ChannelError: types.IsChannelError(err), SkipRetry: types.IsSkipRetryError(err)}
}

func ShouldDisableChannel(err *types.NewAPIError) bool {
	return channelFailurePolicy().ShouldDisable(channelFailure(err))
}
func ShouldEnableChannel(err *types.NewAPIError, status int) bool {
	return channelFailurePolicy().ShouldEnable(err != nil, status)
}

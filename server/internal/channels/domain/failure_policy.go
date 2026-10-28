package domain

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// ChannelFailure describes a failed channel request in gateway-neutral terms.
type ChannelFailure struct {
	Message string
	Status  int
	// ChannelError marks a failure of the channel itself (credentials,
	// configuration), SkipRetry a failure the gateway does not retry.
	ChannelError, SkipRetry bool
}

type FailurePolicy struct {
	DisableEnabled, EnableEnabled func() bool
	DisableStatus                 func(int) bool
	Keywords                      func() []string
	Search                        func(string, []string, bool) (bool, []string)
}

// ShouldDisable decides whether failure disables the channel; nil means the
// request succeeded.
func (policy FailurePolicy) ShouldDisable(failure *ChannelFailure) bool {
	if !policy.DisableEnabled() {
		return false
	}
	if failure == nil {
		return false
	}
	if failure.ChannelError {
		return true
	}
	if failure.SkipRetry {
		return false
	}
	if policy.DisableStatus(failure.Status) {
		return true
	}

	lowerMessage := strings.ToLower(failure.Message)
	search, _ := policy.Search(lowerMessage, policy.Keywords(), true)
	return search
}

// ShouldEnable decides whether a successful request re-enables an
// automatically disabled channel.
func (policy FailurePolicy) ShouldEnable(failed bool, status int) bool {
	if !policy.EnableEnabled() {
		return false
	}
	if failed {
		return false
	}
	if status != constant.ChannelStatusAutoDisabled {
		return false
	}
	return true
}

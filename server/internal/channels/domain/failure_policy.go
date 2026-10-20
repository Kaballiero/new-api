package domain

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"strings"
)

type FailurePolicy struct {
	DisableEnabled, EnableEnabled func() bool
	DisableStatus                 func(int) bool
	Keywords                      func() []string
	Search                        func(string, []string, bool) (bool, []string)
}

func (policy FailurePolicy) ShouldDisable(err *types.NewAPIError) bool {
	if !policy.DisableEnabled() {
		return false
	}
	if err == nil {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if types.IsSkipRetryError(err) {
		return false
	}
	if policy.DisableStatus(err.StatusCode) {
		return true
	}

	lowerMessage := strings.ToLower(err.Error())
	search, _ := policy.Search(lowerMessage, policy.Keywords(), true)
	return search
}

func (policy FailurePolicy) ShouldEnable(newAPIError *types.NewAPIError, status int) bool {
	if !policy.EnableEnabled() {
		return false
	}
	if newAPIError != nil {
		return false
	}
	if status != common.ChannelStatusAutoDisabled {
		return false
	}
	return true
}

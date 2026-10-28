package domain

import (
	"errors"
	"strconv"
)

var (
	ErrKeyDisabled      = errors.New("api key is disabled")
	ErrOwnerBanned      = errors.New("api key owner is banned")
	ErrIPInvalid        = errors.New("client IP is invalid")
	ErrIPDenied         = errors.New("client IP is not allowed")
	ErrGroupForbidden   = errors.New("api key group is not allowed")
	ErrGroupDeprecated  = errors.New("api key group is deprecated")
	ErrChannelInvalid   = errors.New("invalid specific channel id")
	ErrChannelForbidden = errors.New("Specifying a channel is not supported for regular users")
)

const OwnerStatusEnabled = 1

// Owner is the account snapshot needed by gateway authentication and its context.
type Owner struct {
	ID                              int
	Group, Email, Username, Setting string
	Quota, Status                   int
}

// Caller preserves completed authentication steps, including on rejection.
// The gateway writes only the context whose corresponding step completed.
type Caller struct {
	Token      *Token
	Owner      *Owner
	UsingGroup *string
	Context    *KeyContext
}

type KeyContext struct {
	Token         *Token
	ModelLimits   map[string]bool
	AutoGroups    []string
	HasAutoGroups bool
	ChannelID     *int
}

// ContextForKey also serves the dashboard playground's trusted temporary key.
// Model restrictions are passed to the gateway; channel selection enforces them.
func ContextForKey(token *Token, groups []string, present bool, channel *string, admin bool) (*KeyContext, error) {
	result := &KeyContext{Token: token, AutoGroups: groups, HasAutoGroups: present}
	if token.ModelLimitsEnabled {
		result.ModelLimits = token.GetModelLimitsMap()
	}
	if channel == nil {
		return result, nil
	}
	if !admin {
		return result, ErrChannelForbidden
	}
	id, err := strconv.Atoi(*channel)
	if err != nil {
		return result, ErrChannelInvalid
	}
	result.ChannelID = &id
	return result, nil
}

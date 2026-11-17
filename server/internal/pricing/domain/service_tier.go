package domain

import "strings"

// ServiceTier is the OpenRouter service tier of an endpoint or a request.
type ServiceTier string

const (
	ServiceTierDefault   ServiceTier = "default"
	ServiceTierPriority  ServiceTier = "priority"
	ServiceTierFlex      ServiceTier = "flex"
	ServiceTierUltrafast ServiceTier = "ultrafast"
)

// EndpointServiceTier is the service tier an OpenRouter endpoint tag names, in
// any letter case: the /priority or /fast suffix names the priority tier, /flex
// and /ultrafast their tiers. Any other tag, including an ordinary variant such
// as deepinfra/turbo, is the default tier.
func EndpointServiceTier(tag string) ServiceTier {
	tag = strings.ToLower(tag)
	switch {
	case strings.HasSuffix(tag, "/priority"), strings.HasSuffix(tag, "/fast"):
		return ServiceTierPriority
	case strings.HasSuffix(tag, "/flex"):
		return ServiceTierFlex
	case strings.HasSuffix(tag, "/ultrafast"):
		return ServiceTierUltrafast
	}
	return ServiceTierDefault
}

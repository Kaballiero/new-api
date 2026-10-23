package domain

// Audience is a log reader's authority, independently of the stored representation.
type Audience uint8

const (
	AudienceUser Audience = iota
	AudienceAdmin
	AudienceRoot
)

// VisibilityPolicy contains permissions, never persisted metadata or JSON shapes.
type VisibilityPolicy struct {
	HidePrivileged    bool
	HideRoot          bool
	HideChannelNames  bool
	ReplaceDisplayIDs bool
}

func LogVisibility(audience Audience) VisibilityPolicy {
	switch audience {
	case AudienceRoot:
		return VisibilityPolicy{}
	case AudienceAdmin:
		return VisibilityPolicy{HideRoot: true}
	default:
		return VisibilityPolicy{HidePrivileged: true, HideRoot: true, HideChannelNames: true, ReplaceDisplayIDs: true}
	}
}

package domain

// AutoGroupRules validates and stores the auto groups of a key. Failures to
// resolve the user group or to encode the groups are returned unchanged.
type AutoGroupRules struct {
	Max        func() int
	UserGroup  func() (string, error)
	Selectable func(string, string) bool
	Encode     func(*Token, []string) error
}

func (r AutoGroupRules) Apply(t *Token, groups []string) error {
	if len(groups) == 0 {
		return r.Encode(t, nil)
	}
	max := r.Max()
	if len(groups) > max {
		return &LimitError{Err: ErrAutoGroupsTooMany, Max: max}
	}
	userGroup, err := r.UserGroup()
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if _, ok := seen[group]; ok {
			return &GroupError{Err: ErrAutoGroupDuplicate, Group: group}
		}
		seen[group] = struct{}{}
		if !r.Selectable(userGroup, group) {
			return &GroupError{Err: ErrAutoGroupInvalid, Group: group}
		}
	}
	return r.Encode(t, groups)
}

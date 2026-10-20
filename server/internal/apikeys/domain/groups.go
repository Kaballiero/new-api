package domain

type AutoGroupRules struct {
	Max        func() int
	UserGroup  func() (string, error)
	Selectable func(string, string) bool
	Encode     func(*Token, []string) error
}

func (r AutoGroupRules) Apply(t *Token, groups []string) *Failure {
	if len(groups) == 0 {
		if e := r.Encode(t, nil); e != nil {
			return &Failure{Kind: "plain", Cause: e}
		}
		return nil
	}
	max := r.Max()
	if len(groups) > max {
		return &Failure{Kind: "auto_too_many", Params: map[string]any{"Max": max}}
	}
	userGroup, e := r.UserGroup()
	if e != nil {
		return &Failure{Kind: "plain", Cause: e}
	}
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if _, ok := seen[group]; ok {
			return &Failure{Kind: "auto_duplicate", Params: map[string]any{"Group": group}}
		}
		seen[group] = struct{}{}
		if !r.Selectable(userGroup, group) {
			return &Failure{Kind: "auto_invalid", Params: map[string]any{"Group": group}}
		}
	}
	if e := r.Encode(t, groups); e != nil {
		return &Failure{Kind: "plain", Cause: e}
	}
	return nil
}

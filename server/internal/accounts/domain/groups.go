package domain

import (
	"strings"
)

// GroupLabels are the texts of the generated group entries: the description of
// the user's own group missing from the usable groups and the ratio of "auto".
// The transport passes them translated for the request.
type GroupLabels struct {
	UserGroup string
	AutoRatio string
}

// Groups reads the current settings at the previous points where account group
// rules apply.
type Groups struct {
	Labels        GroupLabels
	UsableCopy    func() map[string]string
	SpecialUsable func(string) (map[string]string, bool)
	ContainsRatio func(string) bool
	AutoGroups    func() []string
	MaxAutoGroups func() int
	RatiosCopy    func() map[string]float64
	Ratio         func(string, string) float64
	Description   func(string) string
}

func (groups Groups) Usable(userGroup string) map[string]string {
	groupsCopy := groups.UsableCopy()
	if userGroup != "" {
		specialSettings, b := groups.SpecialUsable(userGroup)
		if b {
			for specialGroup, desc := range specialSettings {
				if after, ok := strings.CutPrefix(specialGroup, "-:"); ok {
					groupToRemove := after
					delete(groupsCopy, groupToRemove)
				} else if after, ok := strings.CutPrefix(specialGroup, "+:"); ok {
					groupToAdd := after
					groupsCopy[groupToAdd] = desc
				} else {
					groupsCopy[specialGroup] = desc
				}
			}
		}
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = groups.Labels.UserGroup
		}
	}
	return groupsCopy
}

func (groups Groups) UsableContains(userGroup, groupName string) bool {
	_, ok := groups.Usable(userGroup)[groupName]
	return ok
}

func (groups Groups) Selectable(userGroup, groupName string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return groups.UsableContains(userGroup, groupName) && groups.ContainsRatio(groupName)
}

func (groups Groups) Auto(userGroup string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range groups.AutoGroups() {
		if !groups.Selectable(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

func (groups Groups) FilterTokenAuto(userGroup string, candidates []string) []string {
	maxCount := groups.MaxAutoGroups()
	filtered := make([]string, 0, min(len(candidates), maxCount))
	seen := make(map[string]struct{})
	for _, group := range candidates {
		if !groups.Selectable(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

func (groups Groups) Names() []string {
	groupNames := make([]string, 0)
	for groupName := range groups.RatiosCopy() {
		groupNames = append(groupNames, groupName)
	}
	return groupNames
}

func (groups Groups) ForUser(userGroup string) map[string]map[string]any {
	usableGroups := make(map[string]map[string]any)
	userUsableGroups := groups.Usable(userGroup)
	for groupName := range groups.RatiosCopy() {
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]any{
				"ratio": groups.Ratio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]any{
			"ratio": groups.Labels.AutoRatio,
			"desc":  groups.Description("auto"),
		}
	}
	return usableGroups
}

// ModelGroups selects the groups of a request and keeps the map iteration order.
func (groups Groups) ModelGroups(userGroup, selected string) []string {
	usable := groups.Usable(userGroup)
	var result []string
	switch {
	case selected == "":
		for group := range usable {
			result = append(result, group)
		}
	case selected == "auto":
		if _, ok := usable[selected]; ok {
			result = groups.Auto(userGroup)
		}
	default:
		if _, ok := usable[selected]; ok {
			result = []string{selected}
		}
	}
	return result
}

package accountssettings

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// UserGroups assembles the account group rules with the sources of the current
// settings. Consumers outside the accounts transport (service, pricing, relay)
// are not localized yet and keep the previous zh-CN labels; the accounts
// transport replaces Labels with the texts of the request language.
func UserGroups() domain.Groups {
	return domain.Groups{
		Labels: domain.GroupLabels{
			UserGroup: i18n.Translate(i18n.LangZhCN, i18n.MsgAccountUserGroupLabel),
			AutoRatio: i18n.Translate(i18n.LangZhCN, i18n.MsgAccountAutoGroupRatio),
		},
		UsableCopy: setting.GetUserUsableGroupsCopy,
		SpecialUsable: func(userGroup string) (map[string]string, bool) {
			return ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		},
		ContainsRatio: ratio_setting.ContainsGroupRatio,
		AutoGroups:    setting.GetAutoGroups,
		MaxAutoGroups: setting.GetMaxTokenAutoGroups,
		RatiosCopy:    ratio_setting.GetGroupRatioCopy,
		Ratio:         GroupRatio,
		Description:   setting.GetUsableGroupDescription,
	}
}

func GetUserUsableGroups(userGroup string) map[string]string {
	return UserGroups().Usable(userGroup)
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	return UserGroups().UsableContains(userGroup, groupName)
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	return UserGroups().Selectable(userGroup, groupName)
}

func GetUserAutoGroup(userGroup string) []string {
	return UserGroups().Auto(userGroup)
}

func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	return UserGroups().FilterTokenAuto(userGroup, groups)
}

func GroupRatio(userGroup, group string) float64 {
	ratio, ok := ratio_setting.GetGroupGroupRatio(userGroup, group)
	if ok {
		return ratio
	}
	return ratio_setting.GetGroupRatio(group)
}

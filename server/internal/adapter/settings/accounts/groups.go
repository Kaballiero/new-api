package accountssettings

import (
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// UserGroups собирает правила аккаунта с источниками актуальных настроек.
func UserGroups() domain.Groups {
	return domain.Groups{
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
